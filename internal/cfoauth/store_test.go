package cfoauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cfui/internal/persist"
)

func TestStorePersistsOAuthStateAndSessionInSQLite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store := NewStore(dir)
	pending := PendingState{
		State:        "state-1",
		CodeVerifier: "verifier-1",
		RedirectURI:  "https://oauth.example.test/oauth/callback",
		Scope:        "dns.read dns.write",
		ExpiresAt:    time.Now().UTC().Add(time.Minute),
	}
	if err := store.SaveState(ctx, pending); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	session := Session{
		ID:           "session-1",
		Label:        "me@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		Scope:        "dns.read dns.write",
		Current:      true,
	}
	if err := store.SaveSession(ctx, session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	closeStore(t, store)
	assertOAuthRowsInSQLite(t, dir, session, pending)

	reloaded := NewStore(dir)
	t.Cleanup(func() { closeStore(t, reloaded) })

	consumed, err := reloaded.ConsumeState(ctx, pending.State)
	if err != nil {
		t.Fatalf("ConsumeState after reload: %v", err)
	}
	if consumed.CodeVerifier != pending.CodeVerifier || consumed.RedirectURI != pending.RedirectURI || consumed.Scope != pending.Scope {
		t.Fatalf("unexpected consumed state: %#v", consumed)
	}
	if _, err := reloaded.ConsumeState(ctx, pending.State); err != ErrStateExpired {
		t.Fatalf("expected consumed state to be deleted, got %v", err)
	}

	current, err := reloaded.CurrentSession(ctx)
	if err != nil {
		t.Fatalf("CurrentSession after reload: %v", err)
	}
	if current.ID != session.ID || current.AccessToken != session.AccessToken || current.RefreshToken != session.RefreshToken {
		t.Fatalf("unexpected current session: %#v", current)
	}

	assertNoJSONFiles(t, dir)
}

func assertOAuthRowsInSQLite(t *testing.T, dir string, session Session, pending PendingState) {
	t.Helper()

	db, err := persist.OpenRawDB(dir)
	if err != nil {
		t.Fatalf("OpenRawDB: %v", err)
	}
	defer db.Close()

	var sessionRows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM oauth_sessions WHERE session_id = ? AND access_token = ? AND refresh_token = ?`,
		session.ID,
		session.AccessToken,
		session.RefreshToken,
	).Scan(&sessionRows); err != nil {
		t.Fatalf("query oauth_sessions: %v", err)
	}
	if sessionRows != 1 {
		t.Fatalf("expected OAuth session to be stored in SQLite, got %d rows", sessionRows)
	}

	var stateRows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM oauth_states WHERE state = ? AND code_verifier = ? AND redirect_uri = ?`,
		pending.State,
		pending.CodeVerifier,
		pending.RedirectURI,
	).Scan(&stateRows); err != nil {
		t.Fatalf("query oauth_states: %v", err)
	}
	if stateRows != 1 {
		t.Fatalf("expected OAuth state to be stored in SQLite, got %d rows", stateRows)
	}
}

func TestStoreSaveSessionKeepsExactlyOneCurrentSession(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	t.Cleanup(func() { closeStore(t, store) })

	first := Session{
		ID:          "first",
		Label:       "First",
		AccessToken: "access-1",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		Scope:       "zone.read",
	}
	second := Session{
		ID:          "second",
		Label:       "Second",
		AccessToken: "access-2",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		Scope:       "dns.read",
	}
	if err := store.SaveSession(ctx, first); err != nil {
		t.Fatalf("SaveSession first: %v", err)
	}
	if err := store.SaveSession(ctx, second); err != nil {
		t.Fatalf("SaveSession second: %v", err)
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %#v", sessions)
	}
	currentCount := 0
	for _, session := range sessions {
		if session.Current {
			currentCount++
			if session.ID != "second" {
				t.Fatalf("expected second session current, got %#v", session)
			}
		}
	}
	if currentCount != 1 {
		t.Fatalf("expected exactly one current session, got %d in %#v", currentCount, sessions)
	}
}

func TestStoreDeleteCurrentPromotesOldestRemainingSession(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	t.Cleanup(func() { closeStore(t, store) })

	first := Session{ID: "first", Label: "First", AccessToken: "access-1", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	second := Session{ID: "second", Label: "Second", AccessToken: "access-2", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := store.SaveSession(ctx, first); err != nil {
		t.Fatalf("SaveSession first: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := store.SaveSession(ctx, second); err != nil {
		t.Fatalf("SaveSession second: %v", err)
	}

	if err := store.DeleteSession(ctx, "second"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	current, err := store.CurrentSession(ctx)
	if err != nil {
		t.Fatalf("CurrentSession: %v", err)
	}
	if current.ID != "first" {
		t.Fatalf("expected first session to be promoted, got %#v", current)
	}
}

func TestStoreSwitchSessionKeepsExactlyOneCurrentSession(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	t.Cleanup(func() { closeStore(t, store) })

	first := Session{ID: "first", Label: "First", AccessToken: "access-1", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	second := Session{ID: "second", Label: "Second", AccessToken: "access-2", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := store.SaveSession(ctx, first); err != nil {
		t.Fatalf("SaveSession first: %v", err)
	}
	if err := store.SaveSession(ctx, second); err != nil {
		t.Fatalf("SaveSession second: %v", err)
	}
	if err := store.SwitchSession(ctx, "first"); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	current, err := store.CurrentSession(ctx)
	if err != nil {
		t.Fatalf("CurrentSession: %v", err)
	}
	if current.ID != "first" {
		t.Fatalf("expected first current, got %#v", current)
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	currentCount := 0
	for _, session := range sessions {
		if session.Current {
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Fatalf("expected exactly one current session, got %d in %#v", currentCount, sessions)
	}
	if err := store.SwitchSession(ctx, "missing"); err != ErrNotLoggedIn {
		t.Fatalf("expected ErrNotLoggedIn for missing session, got %v", err)
	}
}

func TestStoreUpdateSessionLabelOnlyChangesTargetLabel(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	t.Cleanup(func() { closeStore(t, store) })

	first := Session{ID: "first", Label: "First", AccessToken: "access-1", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	second := Session{ID: "second", Label: "Second", AccessToken: "access-2", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := store.SaveSession(ctx, first); err != nil {
		t.Fatalf("SaveSession first: %v", err)
	}
	if err := store.SaveSession(ctx, second); err != nil {
		t.Fatalf("SaveSession second: %v", err)
	}

	if err := store.UpdateSessionLabel(ctx, "first", "  Primary Cloudflare  "); err != nil {
		t.Fatalf("UpdateSessionLabel: %v", err)
	}
	updated, err := store.Session(ctx, "first")
	if err != nil {
		t.Fatalf("Session first: %v", err)
	}
	if updated.Label != "Primary Cloudflare" || updated.AccessToken != first.AccessToken {
		t.Fatalf("unexpected updated session: %#v", updated)
	}
	untouched, err := store.Session(ctx, "second")
	if err != nil {
		t.Fatalf("Session second: %v", err)
	}
	if untouched.Label != "Second" {
		t.Fatalf("unexpected second session label: %#v", untouched)
	}
	if err := store.UpdateSessionLabel(ctx, "missing", "Name"); err != ErrNotLoggedIn {
		t.Fatalf("expected ErrNotLoggedIn for missing session, got %v", err)
	}
}

func TestStoreValidationReportArchivesPersistSanitizedSnapshots(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewStore(dir)
	t.Cleanup(func() { closeStore(t, store) })

	generatedAt := time.Date(2026, 6, 19, 10, 30, 0, 0, time.UTC)
	reportBody := []byte(`{"version":1,"generated_at":"2026-06-19T10:30:00Z","contains_oauth_token":false,"contains_refresh_token":false,"requested_template_scopes":["zone.read","dns.read","dns.read"],"session":{"scopes":["dns.read","zone.read","workers-r2.read"]},"summary":{"scope_missing":2,"api_unavailable":1}}`)
	saved, err := store.SaveValidationReportArchive(ctx, ValidationReportArchiveInput{
		SessionID:       "session-1",
		SessionLabel:    "Production",
		AccountID:       "account-1",
		AccountName:     "Example Account",
		ZoneID:          "zone-1",
		ZoneName:        "example.com",
		GeneratedAt:     generatedAt,
		ScopeMissing:    2,
		APIUnavailable:  1,
		APIMissingScope: -1,
		ActionItems:     3,
		ReportBody:      reportBody,
	})
	if err != nil {
		t.Fatalf("SaveValidationReportArchive: %v", err)
	}
	if saved.ReportID == "" || saved.SessionID != "session-1" || saved.AccountName != "Example Account" || !saved.GeneratedAt.Equal(generatedAt) {
		t.Fatalf("unexpected saved archive: %#v", saved)
	}
	if saved.APIMissingScope != 0 {
		t.Fatalf("negative counters should be normalized, got %#v", saved)
	}
	if saved.RequestedScopes != 2 || saved.GrantedScopes != 3 || saved.RequestedHash == "" || saved.GrantedHash == "" || saved.RequestedHash == saved.GrantedHash {
		t.Fatalf("unexpected scope summary: %#v", saved)
	}
	if string(saved.Report) != string(reportBody) {
		t.Fatalf("saved report body mismatch: %s", saved.Report)
	}

	items, err := store.ListValidationReportArchives(ctx, 12)
	if err != nil {
		t.Fatalf("ListValidationReportArchives: %v", err)
	}
	if len(items) != 1 || items[0].ReportID != saved.ReportID || items[0].ScopeMissing != 2 {
		t.Fatalf("unexpected archive summaries: %#v", items)
	}
	if items[0].RequestedScopes != saved.RequestedScopes || items[0].GrantedScopes != saved.GrantedScopes || items[0].RequestedHash != saved.RequestedHash || items[0].GrantedHash != saved.GrantedHash {
		t.Fatalf("archive list did not include stable scope summary: %#v", items[0])
	}
	publicList, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("Marshal archive summaries: %v", err)
	}
	if strings.Contains(string(publicList), "contains_oauth_token") || strings.Contains(string(publicList), "requested_template_scopes") || strings.Contains(string(publicList), "workers-r2.read") || strings.Contains(string(publicList), "summary") || strings.Contains(string(publicList), "access-token") || strings.Contains(string(publicList), "refresh-token") {
		t.Fatalf("archive summaries leaked report or token material: %s", publicList)
	}

	detail, err := store.ValidationReportArchive(ctx, saved.ReportID)
	if err != nil {
		t.Fatalf("ValidationReportArchive: %v", err)
	}
	if string(detail.Report) != string(reportBody) || detail.ReportID != saved.ReportID {
		t.Fatalf("unexpected archive detail: %#v", detail)
	}

	db, err := persist.OpenRawDB(dir)
	if err != nil {
		t.Fatalf("OpenRawDB: %v", err)
	}
	defer db.Close()
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM oauth_validation_reports WHERE report_id = ? AND report_body = ?`, saved.ReportID, string(reportBody)).Scan(&rows); err != nil {
		t.Fatalf("query oauth_validation_reports: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected validation report archive in SQLite, got %d rows", rows)
	}

	if err := store.DeleteValidationReportArchive(ctx, saved.ReportID); err != nil {
		t.Fatalf("DeleteValidationReportArchive: %v", err)
	}
	if _, err := store.ValidationReportArchive(ctx, saved.ReportID); !errors.Is(err, ErrValidationReportNotFound) {
		t.Fatalf("expected ErrValidationReportNotFound after delete, got %v", err)
	}
	if err := store.DeleteValidationReportArchive(ctx, saved.ReportID); !errors.Is(err, ErrValidationReportNotFound) {
		t.Fatalf("expected ErrValidationReportNotFound for missing delete, got %v", err)
	}

	assertNoJSONFiles(t, dir)
}

func TestStoreValidationReportArchiveRejectsUnsafeBodies(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	t.Cleanup(func() { closeStore(t, store) })

	tests := []struct {
		name  string
		input ValidationReportArchiveInput
		want  string
	}{
		{
			name:  "oauth token flag",
			input: ValidationReportArchiveInput{ContainsToken: true, ReportBody: []byte(`{"ok":true}`)},
			want:  "oauth tokens",
		},
		{
			name:  "refresh token flag",
			input: ValidationReportArchiveInput{ContainsRefresh: true, ReportBody: []byte(`{"ok":true}`)},
			want:  "oauth tokens",
		},
		{
			name:  "empty body",
			input: ValidationReportArchiveInput{},
			want:  "required",
		},
		{
			name:  "invalid json",
			input: ValidationReportArchiveInput{ReportBody: []byte(`not json`)},
			want:  "valid json",
		},
		{
			name:  "token field in body",
			input: ValidationReportArchiveInput{ReportBody: []byte(`{"report":{"access_token":"secret"}}`)},
			want:  "token fields",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.SaveValidationReportArchive(ctx, tt.input); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func closeStore(t *testing.T, store *Store) {
	t.Helper()
	if store != nil && store.client != nil {
		if err := store.client.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}
}

func assertNoJSONFiles(t *testing.T, dir string) {
	t.Helper()
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			t.Fatalf("OAuth store must not write JSON files, found %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}
