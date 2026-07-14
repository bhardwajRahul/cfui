package configbackup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	encryptionAlgorithm = "AES-256-GCM"
	encryptionKDF       = "scrypt"
	scryptN             = 32768
	scryptR             = 8
	scryptP             = 1
	saltBytes           = 16
	nonceBytes          = 12
)

var backupAdditionalData = []byte("cfui-config-backup:v1")

func Encode(payload Payload, password string, random io.Reader) ([]byte, error) {
	if err := validatePayload(payload); err != nil {
		return nil, err
	}
	if password == "" {
		return json.MarshalIndent(Envelope{
			Format:    Format,
			Version:   EnvelopeVersion,
			Encrypted: false,
			Payload:   &payload,
		}, "", "  ")
	}

	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encode payload", ErrInvalidBackup)
	}
	salt := make([]byte, saltBytes)
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(random, salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(random, nonce); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, backupAdditionalData)
	envelope := Envelope{
		Format:    Format,
		Version:   EnvelopeVersion,
		Encrypted: true,
		Encryption: &Encryption{
			Algorithm: encryptionAlgorithm,
			KDF:       encryptionKDF,
			N:         scryptN,
			R:         scryptR,
			P:         scryptP,
			Salt:      base64.StdEncoding.EncodeToString(salt),
			Nonce:     base64.StdEncoding.EncodeToString(nonce),
		},
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	return json.MarshalIndent(envelope, "", "  ")
}

func Decode(data []byte, password string) (Decoded, error) {
	if len(data) == 0 || len(data) > MaxBackupBytes {
		return Decoded{}, ErrInvalidBackup
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Decoded{}, fmt.Errorf("%w: duplicate or malformed JSON", ErrInvalidBackup)
	}
	var envelope Envelope
	if err := decodeStrict(data, &envelope); err != nil {
		return Decoded{}, fmt.Errorf("%w: decode envelope", ErrInvalidBackup)
	}
	if envelope.Format != Format {
		return Decoded{}, ErrInvalidBackup
	}
	if envelope.Version != EnvelopeVersion {
		return Decoded{}, ErrUnsupportedVersion
	}

	if !envelope.Encrypted {
		if envelope.Payload == nil || envelope.Encryption != nil || envelope.Ciphertext != "" {
			return Decoded{}, ErrInvalidBackup
		}
		if err := validatePayload(*envelope.Payload); err != nil {
			return Decoded{}, err
		}
		return Decoded{Payload: *envelope.Payload}, nil
	}

	if envelope.Payload != nil || envelope.Encryption == nil || envelope.Ciphertext == "" {
		return Decoded{}, ErrInvalidBackup
	}
	if err := validateEncryption(*envelope.Encryption); err != nil {
		return Decoded{}, err
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.Encryption.Salt)
	if err != nil || len(salt) != saltBytes {
		return Decoded{}, ErrInvalidBackup
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Encryption.Nonce)
	if err != nil || len(nonce) != nonceBytes {
		return Decoded{}, ErrInvalidBackup
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) < 16 {
		return Decoded{}, ErrInvalidBackup
	}
	if password == "" {
		return Decoded{}, ErrPasswordRequired
	}

	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return Decoded{}, ErrInvalidBackup
	}
	gcm, err := newGCM(key)
	if err != nil {
		return Decoded{}, ErrInvalidBackup
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, backupAdditionalData)
	if err != nil {
		return Decoded{}, ErrInvalidPasswordOrTampered
	}
	if len(plain) > MaxBackupBytes || rejectDuplicateJSONKeys(plain) != nil {
		return Decoded{}, ErrInvalidBackup
	}
	var payload Payload
	if err := decodeStrict(plain, &payload); err != nil {
		return Decoded{}, fmt.Errorf("%w: decode payload", ErrInvalidBackup)
	}
	if err := validatePayload(payload); err != nil {
		return Decoded{}, err
	}
	return Decoded{Payload: payload, Encrypted: true}, nil
}

func Inspect(decoded Decoded) Inspection {
	inspection := Inspection{
		CreatedAt:         decoded.Payload.CreatedAt,
		AppVersion:        decoded.Payload.AppVersion,
		Encrypted:         decoded.Encrypted,
		Sections:          append([]Section(nil), decoded.Payload.Sections...),
		ContainsSensitive: decoded.Payload.Sensitive != nil,
	}
	if decoded.Payload.Tunnels != nil {
		inspection.TunnelProfiles = len(decoded.Payload.Tunnels.Profiles)
	}
	if decoded.Payload.DDNS != nil {
		inspection.DDNSSources = len(decoded.Payload.DDNS.IPSources)
		inspection.DDNSRecords = len(decoded.Payload.DDNS.Records)
	}
	if decoded.Payload.S3WebDAV != nil {
		inspection.S3Mounts = len(decoded.Payload.S3WebDAV.Mounts)
	}
	return inspection
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func validateEncryption(encryption Encryption) error {
	if encryption.Algorithm != encryptionAlgorithm || encryption.KDF != encryptionKDF || encryption.N != scryptN || encryption.R != scryptR || encryption.P != scryptP {
		return ErrInvalidBackup
	}
	return nil
}

func validatePayload(payload Payload) error {
	if payload.SchemaVersion != PayloadVersion {
		return ErrUnsupportedVersion
	}
	if payload.CreatedAt.IsZero() {
		return ErrInvalidBackup
	}
	_, offset := payload.CreatedAt.Zone()
	if offset != 0 {
		return ErrInvalidBackup
	}
	if err := validateStringLengths(reflect.ValueOf(payload)); err != nil {
		return err
	}
	if len(payload.Sections) == 0 {
		return ErrInvalidBackup
	}

	selected := make(map[Section]bool, len(payload.Sections))
	lastOrder := -1
	normalSections := 0
	for _, section := range payload.Sections {
		order := sectionIndex(section)
		if order < 0 || selected[section] || order <= lastOrder {
			return ErrInvalidBackup
		}
		selected[section] = true
		lastOrder = order
		if section != SectionSensitive {
			normalSections++
		}
	}
	if normalSections == 0 {
		return ErrInvalidBackup
	}

	if selected[SectionTunnels] != (payload.Tunnels != nil) ||
		selected[SectionRemoteManagement] != (payload.RemoteManagement != nil) ||
		selected[SectionDDNS] != (payload.DDNS != nil) ||
		selected[SectionS3WebDAV] != (payload.S3WebDAV != nil) ||
		selected[SectionApplication] != (payload.Application != nil) ||
		selected[SectionSensitive] != (payload.Sensitive != nil) {
		return ErrInvalidBackup
	}

	if payload.Tunnels != nil {
		if len(payload.Tunnels.Profiles) == 0 || len(payload.Tunnels.Profiles) > MaxTunnelProfiles || hasDuplicateTunnelKeys(payload.Tunnels.Profiles) || !containsTunnelKey(payload.Tunnels.Profiles, payload.Tunnels.ActiveKey) {
			return ErrInvalidBackup
		}
	}
	if payload.RemoteManagement != nil {
		if len(payload.RemoteManagement.Profiles) > MaxTunnelProfiles || hasDuplicateRemoteKeys(payload.RemoteManagement.Profiles) {
			return ErrInvalidBackup
		}
	}
	if payload.DDNS != nil && (len(payload.DDNS.IPSources) > MaxDDNSSources || len(payload.DDNS.Records) > MaxDDNSRecords) {
		return ErrInvalidBackup
	}
	if payload.S3WebDAV != nil {
		if len(payload.S3WebDAV.Mounts) == 0 || len(payload.S3WebDAV.Mounts) > MaxS3Mounts || hasDuplicateS3Keys(payload.S3WebDAV.Mounts) || !containsS3Key(payload.S3WebDAV.Mounts, payload.S3WebDAV.ActiveKey) {
			return ErrInvalidBackup
		}
	}
	if payload.Sensitive != nil {
		if len(payload.Sensitive.TunnelTokens) > MaxTunnelProfiles || len(payload.Sensitive.S3) > MaxS3Mounts || hasEmptyMapKey(payload.Sensitive.TunnelTokens) || hasEmptyMapKey(payload.Sensitive.S3) {
			return ErrInvalidBackup
		}
		if !selected[SectionTunnels] && len(payload.Sensitive.TunnelTokens) > 0 {
			return ErrInvalidBackup
		}
		if !selected[SectionRemoteManagement] && (payload.Sensitive.APIToken != "" || payload.Sensitive.APIKey != "") {
			return ErrInvalidBackup
		}
		if !selected[SectionS3WebDAV] && len(payload.Sensitive.S3) > 0 {
			return ErrInvalidBackup
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > MaxBackupBytes {
		return ErrInvalidBackup
	}
	return nil
}

func sectionIndex(section Section) int {
	for i, candidate := range sectionOrder {
		if section == candidate {
			return i
		}
	}
	return -1
}

func hasDuplicateTunnelKeys(profiles []TunnelProfile) bool {
	seen := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Key) == "" || seen[profile.Key] {
			return true
		}
		seen[profile.Key] = true
	}
	return false
}

func containsTunnelKey(profiles []TunnelProfile, key string) bool {
	for _, profile := range profiles {
		if profile.Key == key {
			return true
		}
	}
	return false
}

func hasDuplicateRemoteKeys(profiles []RemoteProfile) bool {
	seen := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Key) == "" || seen[profile.Key] {
			return true
		}
		seen[profile.Key] = true
	}
	return false
}

func hasDuplicateS3Keys(mounts []S3Mount) bool {
	seen := make(map[string]bool, len(mounts))
	for _, mount := range mounts {
		if strings.TrimSpace(mount.Key) == "" || seen[mount.Key] {
			return true
		}
		seen[mount.Key] = true
	}
	return false
}

func containsS3Key(mounts []S3Mount, key string) bool {
	for _, mount := range mounts {
		if mount.Key == key {
			return true
		}
	}
	return false
}

func hasEmptyMapKey[V any](values map[string]V) bool {
	for key := range values {
		if strings.TrimSpace(key) == "" {
			return true
		}
	}
	return false
}

func validateStringLengths(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		return nil
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return validateStringLengths(value.Elem())
	case reflect.String:
		if len(value.String()) > MaxStringBytes {
			return ErrInvalidBackup
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if err := validateStringLengths(value.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := validateStringLengths(value.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateStringLengths(iterator.Key()); err != nil {
				return err
			}
			if err := validateStringLengths(iterator.Value()); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walkValue func() error
	walkValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("duplicate object member")
				}
				seen[key] = true
				if err := walkValue(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("unterminated object")
			}
		case '[':
			for decoder.More() {
				if err := walkValue(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("unterminated array")
			}
		default:
			return errors.New("unexpected delimiter")
		}
		return nil
	}
	if err := walkValue(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}
