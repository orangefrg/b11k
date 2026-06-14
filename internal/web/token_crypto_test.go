package web

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testTokenEncryptionKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestSecretBoxRoundTrip(t *testing.T) {
	box, err := newSecretBox(testTokenEncryptionKey())
	if err != nil {
		t.Fatal(err)
	}
	s := &server{secretBox: box}

	encrypted, err := s.encryptSecret("refresh-token-secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "refresh-token-secret" {
		t.Fatal("secret was not encrypted")
	}
	if !strings.HasPrefix(encrypted, encryptedSecretPrefix) {
		t.Fatalf("encrypted value has unexpected prefix: %q", encrypted)
	}

	decrypted, err := s.decryptSecret(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "refresh-token-secret" {
		t.Fatalf("decrypted value = %q, want original", decrypted)
	}
}

func TestSecretBoxUsesRandomNonce(t *testing.T) {
	box, err := newSecretBox(testTokenEncryptionKey())
	if err != nil {
		t.Fatal(err)
	}
	s := &server{secretBox: box}

	first, err := s.encryptSecret("same-token")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.encryptSecret("same-token")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("encrypting the same token twice produced identical ciphertext")
	}
}

func TestSecretBoxPlaintextCompatibility(t *testing.T) {
	box, err := newSecretBox(testTokenEncryptionKey())
	if err != nil {
		t.Fatal(err)
	}
	s := &server{secretBox: box}

	got, err := s.decryptSecret("legacy-plaintext-refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy-plaintext-refresh-token" {
		t.Fatalf("plaintext compatibility returned %q", got)
	}
}

func TestSecretBoxRequiresKeyForEncryptedValue(t *testing.T) {
	s := &server{}
	_, err := s.decryptSecret(encryptedSecretPrefix + "payload")
	if err == nil {
		t.Fatal("decrypting encrypted value without key should fail")
	}
}

func TestSecretBoxRejectsBadKey(t *testing.T) {
	if _, err := newSecretBox("not-a-valid-key"); err == nil {
		t.Fatal("invalid token encryption key was accepted")
	}
}

func TestRequiresTokenEncryptionForPublicHTTPS(t *testing.T) {
	if !requiresTokenEncryption(Config{WebProtocol: "https", PublicAPIHost: "api.b11k.example.com"}) {
		t.Fatal("public HTTPS mobile API should require token encryption")
	}
	if requiresTokenEncryption(Config{WebProtocol: "http", PublicAPIHost: "api.b11k.example.com"}) {
		t.Fatal("local HTTP development should not require token encryption")
	}
	if requiresTokenEncryption(Config{WebProtocol: "https"}) {
		t.Fatal("web-only HTTPS config without public mobile API should not require token encryption")
	}
}
