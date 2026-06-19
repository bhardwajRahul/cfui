package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cfui/internal/cfaccount"
)

const (
	defaultR2UploadChunkBytes = 8 * 1024 * 1024
	minR2UploadChunkBytes     = 1024 * 1024
	maxR2UploadChunkBytes     = 64 * 1024 * 1024
	maxR2ChunkedUploadBytes   = 5 * 1024 * 1024 * 1024
	r2UploadSessionTTL        = 24 * time.Hour
)

type r2UploadStartRequest struct {
	AccountID   string `json:"account_id"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
	ChunkSize   int64  `json:"chunk_size,omitempty"`
}

type r2UploadStatus struct {
	UploadID       string `json:"upload_id"`
	AccountID      string `json:"account_id"`
	Bucket         string `json:"bucket"`
	Key            string `json:"key"`
	ContentType    string `json:"content_type"`
	Size           int64  `json:"size"`
	ChunkSize      int64  `json:"chunk_size"`
	TotalChunks    int    `json:"total_chunks"`
	ReceivedChunks int    `json:"received_chunks"`
	ReceivedBytes  int64  `json:"received_bytes"`
	Complete       bool   `json:"complete"`
}

type r2UploadManager struct {
	mu       sync.Mutex
	sessions map[string]*r2UploadSession
}

type r2UploadSession struct {
	ID          string
	AccountID   string
	Bucket      string
	Key         string
	ContentType string
	Size        int64
	ChunkSize   int64
	TotalChunks int
	Received    []bool
	TempDir     string
	FilePath    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Completing  bool
}

func newR2UploadManager() *r2UploadManager {
	return &r2UploadManager{sessions: map[string]*r2UploadSession{}}
}

func (s *Server) ensureR2UploadManager() *r2UploadManager {
	if s.r2Uploads == nil {
		s.r2Uploads = newR2UploadManager()
	}
	return s.r2Uploads
}

func (m *r2UploadManager) start(req r2UploadStartRequest) (r2UploadStatus, error) {
	req.AccountID = strings.TrimSpace(req.AccountID)
	req.Bucket = strings.TrimSpace(req.Bucket)
	req.Key = strings.TrimSpace(req.Key)
	if req.AccountID == "" {
		return r2UploadStatus{}, fmt.Errorf("account_id is required")
	}
	if req.Bucket == "" {
		return r2UploadStatus{}, fmt.Errorf("bucket is required")
	}
	if req.Key == "" {
		return r2UploadStatus{}, fmt.Errorf("r2 object key is required")
	}
	if len([]byte(req.Key)) > 1024 {
		return r2UploadStatus{}, fmt.Errorf("r2 object key is too long")
	}
	if req.Size < 0 {
		return r2UploadStatus{}, fmt.Errorf("r2 object upload size is required")
	}
	if req.Size > maxR2ChunkedUploadBytes {
		return r2UploadStatus{}, fmt.Errorf("r2 object upload is too large")
	}
	chunkSize := req.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultR2UploadChunkBytes
	}
	if chunkSize < minR2UploadChunkBytes || chunkSize > maxR2UploadChunkBytes {
		return r2UploadStatus{}, fmt.Errorf("r2 upload chunk size must be between 1 MiB and 64 MiB")
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	id, err := randomUploadID()
	if err != nil {
		return r2UploadStatus{}, err
	}
	tempDir, err := os.MkdirTemp("", "cfui-r2-upload-*")
	if err != nil {
		return r2UploadStatus{}, err
	}
	filePath := filepath.Join(tempDir, id+".part")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return r2UploadStatus{}, err
	}
	if err := file.Truncate(req.Size); err != nil {
		_ = file.Close()
		_ = os.RemoveAll(tempDir)
		return r2UploadStatus{}, err
	}
	if err := file.Close(); err != nil {
		_ = os.RemoveAll(tempDir)
		return r2UploadStatus{}, err
	}
	totalChunks := 1
	if req.Size > 0 {
		totalChunks = int((req.Size + chunkSize - 1) / chunkSize)
	}
	now := time.Now().UTC()
	session := &r2UploadSession{
		ID:          id,
		AccountID:   req.AccountID,
		Bucket:      req.Bucket,
		Key:         req.Key,
		ContentType: contentType,
		Size:        req.Size,
		ChunkSize:   chunkSize,
		TotalChunks: totalChunks,
		Received:    make([]bool, totalChunks),
		TempDir:     tempDir,
		FilePath:    filePath,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.pruneExpired(now)
	m.mu.Lock()
	m.sessions[id] = session
	m.mu.Unlock()
	return session.status(), nil
}

func (m *r2UploadManager) writeChunk(uploadID string, index int, contentLength int64, body io.Reader) (r2UploadStatus, error) {
	if body == nil {
		return r2UploadStatus{}, fmt.Errorf("r2 upload chunk body is required")
	}
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(uploadID)]
	if session == nil {
		m.mu.Unlock()
		return r2UploadStatus{}, fmt.Errorf("r2 upload session not found")
	}
	if session.Completing {
		m.mu.Unlock()
		return r2UploadStatus{}, fmt.Errorf("r2 upload session is completing")
	}
	if index < 0 || index >= session.TotalChunks {
		m.mu.Unlock()
		return r2UploadStatus{}, fmt.Errorf("r2 upload chunk index is invalid")
	}
	expected := session.expectedChunkBytes(index)
	if contentLength >= 0 && contentLength != expected {
		m.mu.Unlock()
		return r2UploadStatus{}, fmt.Errorf("r2 upload chunk size is invalid")
	}
	filePath := session.FilePath
	offset := int64(index) * session.ChunkSize
	m.mu.Unlock()

	file, err := os.OpenFile(filePath, os.O_WRONLY, 0)
	if err != nil {
		return r2UploadStatus{}, err
	}
	written, writeErr := writeChunkAt(file, body, offset, expected)
	closeErr := file.Close()
	if writeErr != nil {
		return r2UploadStatus{}, writeErr
	}
	if closeErr != nil {
		return r2UploadStatus{}, closeErr
	}
	if written != expected {
		return r2UploadStatus{}, fmt.Errorf("r2 upload chunk size is invalid")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	session = m.sessions[strings.TrimSpace(uploadID)]
	if session == nil {
		return r2UploadStatus{}, fmt.Errorf("r2 upload session not found")
	}
	if !session.Received[index] {
		session.Received[index] = true
	}
	session.UpdatedAt = time.Now().UTC()
	return session.status(), nil
}

func (m *r2UploadManager) complete(ctx context.Context, uploadID string, svc *cfaccount.Service) (cfaccount.R2ObjectValue, error) {
	if svc == nil {
		return cfaccount.R2ObjectValue{}, fmt.Errorf("cloudflare service is not configured")
	}
	uploadID = strings.TrimSpace(uploadID)
	m.mu.Lock()
	session := m.sessions[uploadID]
	if session == nil {
		m.mu.Unlock()
		return cfaccount.R2ObjectValue{}, fmt.Errorf("r2 upload session not found")
	}
	if session.Completing {
		m.mu.Unlock()
		return cfaccount.R2ObjectValue{}, fmt.Errorf("r2 upload session is completing")
	}
	if !session.ready() {
		m.mu.Unlock()
		return cfaccount.R2ObjectValue{}, fmt.Errorf("r2 upload session is incomplete")
	}
	session.Completing = true
	filePath := session.FilePath
	accountID := session.AccountID
	bucket := session.Bucket
	key := session.Key
	contentType := session.ContentType
	size := session.Size
	m.mu.Unlock()

	file, err := os.Open(filePath)
	if err != nil {
		m.markNotCompleting(uploadID)
		return cfaccount.R2ObjectValue{}, err
	}
	resp, err := svc.WriteR2ObjectStream(ctx, accountID, bucket, key, contentType, size, file)
	closeErr := file.Close()
	if err != nil {
		m.markNotCompleting(uploadID)
		return cfaccount.R2ObjectValue{}, err
	}
	if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		m.markNotCompleting(uploadID)
		return cfaccount.R2ObjectValue{}, closeErr
	}
	m.remove(uploadID)
	return resp, nil
}

func (m *r2UploadManager) abort(uploadID string) (r2UploadStatus, error) {
	uploadID = strings.TrimSpace(uploadID)
	m.mu.Lock()
	session := m.sessions[uploadID]
	if session != nil {
		delete(m.sessions, uploadID)
	}
	m.mu.Unlock()
	if session == nil {
		return r2UploadStatus{}, fmt.Errorf("r2 upload session not found")
	}
	status := session.status()
	_ = os.RemoveAll(session.TempDir)
	return status, nil
}

func (m *r2UploadManager) remove(uploadID string) {
	m.mu.Lock()
	session := m.sessions[uploadID]
	if session != nil {
		delete(m.sessions, uploadID)
	}
	m.mu.Unlock()
	if session != nil {
		_ = os.RemoveAll(session.TempDir)
	}
}

func (m *r2UploadManager) markNotCompleting(uploadID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[uploadID]; session != nil {
		session.Completing = false
	}
}

func (m *r2UploadManager) pruneExpired(now time.Time) {
	var dirs []string
	m.mu.Lock()
	for id, session := range m.sessions {
		if now.Sub(session.UpdatedAt) <= r2UploadSessionTTL {
			continue
		}
		delete(m.sessions, id)
		dirs = append(dirs, session.TempDir)
	}
	m.mu.Unlock()
	for _, dir := range dirs {
		_ = os.RemoveAll(dir)
	}
}

func (s *r2UploadSession) expectedChunkBytes(index int) int64 {
	if s.Size == 0 {
		return 0
	}
	offset := int64(index) * s.ChunkSize
	remaining := s.Size - offset
	if remaining < s.ChunkSize {
		return remaining
	}
	return s.ChunkSize
}

func (s *r2UploadSession) ready() bool {
	for _, received := range s.Received {
		if !received {
			return false
		}
	}
	return true
}

func (s *r2UploadSession) status() r2UploadStatus {
	receivedChunks := 0
	var receivedBytes int64
	for i, received := range s.Received {
		if !received {
			continue
		}
		receivedChunks++
		receivedBytes += s.expectedChunkBytes(i)
	}
	return r2UploadStatus{
		UploadID:       s.ID,
		AccountID:      s.AccountID,
		Bucket:         s.Bucket,
		Key:            s.Key,
		ContentType:    s.ContentType,
		Size:           s.Size,
		ChunkSize:      s.ChunkSize,
		TotalChunks:    s.TotalChunks,
		ReceivedChunks: receivedChunks,
		ReceivedBytes:  receivedBytes,
		Complete:       receivedChunks == s.TotalChunks,
	}
}

func writeChunkAt(file *os.File, body io.Reader, offset, expected int64) (int64, error) {
	if expected == 0 {
		var one [1]byte
		n, err := body.Read(one[:])
		if err == nil || n > 0 {
			return int64(n), fmt.Errorf("r2 upload chunk size is invalid")
		}
		if err != io.EOF {
			return 0, err
		}
		return 0, nil
	}
	buf := make([]byte, 64*1024)
	var written int64
	for {
		remaining := expected - written
		if remaining <= 0 {
			var one [1]byte
			n, err := body.Read(one[:])
			if err == nil || n > 0 {
				return written + int64(n), fmt.Errorf("r2 upload chunk size is invalid")
			}
			if err == io.EOF {
				return written, nil
			}
			return written, err
		}
		if int64(len(buf)) > remaining {
			buf = buf[:remaining]
		}
		n, err := body.Read(buf)
		if n > 0 {
			if _, writeErr := file.WriteAt(buf[:n], offset+written); writeErr != nil {
				return written, writeErr
			}
			written += int64(n)
		}
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
	}
}

func randomUploadID() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
