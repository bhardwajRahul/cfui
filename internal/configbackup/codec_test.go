package configbackup

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testPayload() Payload {
	return Payload{
		SchemaVersion: PayloadVersion,
		CreatedAt:     time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		AppVersion:    "v0.9.6",
		Sections:      []Section{SectionApplication},
		Application: &ApplicationSection{
			MCPEnabled:            true,
			OAuthClientID:         "client",
			OAuthRelayCallbackURL: "https://relay.example/oauth/callback",
		},
	}
}

func TestEncodeDecodePlaintext(t *testing.T) {
	payload := testPayload()
	data, err := Encode(payload, "", bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(data, "")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Encrypted {
		t.Fatal("plaintext backup reported encrypted")
	}
	if !payloadEqual(decoded.Payload, payload) {
		t.Fatalf("unexpected decoded payload: %#v", decoded.Payload)
	}
}

func TestEncodeDecodeEncrypted(t *testing.T) {
	payload := testPayload()
	data, err := Encode(payload, "backup password", bytes.NewReader(bytes.Repeat([]byte{2}, 64)))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if bytes.Contains(data, []byte("client")) || bytes.Contains(data, []byte("relay.example")) {
		t.Fatal("encrypted envelope leaked plaintext")
	}

	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("Unmarshal envelope: %v", err)
	}
	if envelope.Encryption == nil || envelope.Encryption.Algorithm != "AES-256-GCM" || envelope.Encryption.KDF != "scrypt" || envelope.Encryption.N != 32768 || envelope.Encryption.R != 8 || envelope.Encryption.P != 1 {
		t.Fatalf("unexpected encryption metadata: %#v", envelope.Encryption)
	}

	decoded, err := Decode(data, "backup password")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.Encrypted || !payloadEqual(decoded.Payload, payload) {
		t.Fatalf("unexpected decoded payload: %#v", decoded)
	}
}

func TestDecodeRejectsWrongPasswordAndTampering(t *testing.T) {
	data, err := Encode(testPayload(), "correct", bytes.NewReader(bytes.Repeat([]byte{3}, 64)))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := Decode(data, "wrong"); !errors.Is(err, ErrInvalidPasswordOrTampered) {
		t.Fatalf("wrong password: expected credential error, got %v", err)
	}

	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("Unmarshal envelope: %v", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		t.Fatalf("Decode ciphertext: %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 0xff
	envelope.Ciphertext = base64.StdEncoding.EncodeToString(ciphertext)
	tampered, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal tampered envelope: %v", err)
	}
	if _, err := Decode(tampered, "correct"); !errors.Is(err, ErrInvalidPasswordOrTampered) {
		t.Fatalf("tampered ciphertext: expected credential error, got %v", err)
	}
}

func TestDecodeRejectsInvalidEnvelopeShapes(t *testing.T) {
	tests := []struct {
		name string
		data string
		want error
	}{
		{name: "duplicate member", data: `{"format":"cfui-config-backup","format":"duplicate","version":1,"encrypted":false,"payload":{}}`, want: ErrInvalidBackup},
		{name: "unsupported envelope", data: `{"format":"cfui-config-backup","version":2,"encrypted":false,"payload":{}}`, want: ErrUnsupportedVersion},
		{name: "trailing value", data: `{"format":"cfui-config-backup","version":1,"encrypted":false,"payload":{}} {}`, want: ErrInvalidBackup},
		{name: "unknown field", data: `{"format":"cfui-config-backup","version":1,"encrypted":false,"payload":{},"extra":true}`, want: ErrInvalidBackup},
		{name: "missing password", data: `{"format":"cfui-config-backup","version":1,"encrypted":true,"encryption":{"algorithm":"AES-256-GCM","kdf":"scrypt","n":32768,"r":8,"p":1,"salt":"AAAAAAAAAAAAAAAAAAAAAA==","nonce":"AAAAAAAAAAAAAAAA"},"ciphertext":"AAAAAAAAAAAAAAAAAAAAAA=="}`, want: ErrPasswordRequired},
		{name: "malformed base64", data: `{"format":"cfui-config-backup","version":1,"encrypted":true,"encryption":{"algorithm":"AES-256-GCM","kdf":"scrypt","n":32768,"r":8,"p":1,"salt":"!","nonce":"!"},"ciphertext":"!"}`, want: ErrInvalidBackup},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.data), "")
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestEncodeRejectsOversizedString(t *testing.T) {
	payload := testPayload()
	payload.Application.OAuthClientID = strings.Repeat("x", MaxStringBytes+1)
	if _, err := Encode(payload, "", bytes.NewReader(nil)); !errors.Is(err, ErrInvalidBackup) {
		t.Fatalf("expected invalid backup, got %v", err)
	}
}

func TestInspectReportsAvailableContent(t *testing.T) {
	payload := testPayload()
	payload.Sections = []Section{SectionTunnels, SectionDDNS, SectionS3WebDAV, SectionApplication, SectionSensitive}
	payload.Tunnels = &TunnelSection{Profiles: []TunnelProfile{{Key: "one"}, {Key: "two"}}}
	payload.DDNS = &DDNSSection{IPSources: []IPSource{{URL: "https://ip.example"}}, Records: []DDNSRecord{{Name: "a.example"}}}
	payload.S3WebDAV = &S3WebDAVSection{Mounts: []S3Mount{{Key: "one"}}}
	payload.Sensitive = &SensitiveSection{TunnelTokens: map[string]string{"one": "secret"}}

	inspection := Inspect(Decoded{Payload: payload, Encrypted: true})
	if !inspection.Encrypted || !inspection.ContainsSensitive || inspection.TunnelProfiles != 2 || inspection.DDNSSources != 1 || inspection.DDNSRecords != 1 || inspection.S3Mounts != 1 {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}
}

func payloadEqual(a, b Payload) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return bytes.Equal(aJSON, bJSON)
}
