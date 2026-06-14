package web

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const encryptedSecretPrefix = "enc:v1:"

type secretBox struct {
	aead cipher.AEAD
}

func newSecretBox(keyText string) (*secretBox, error) {
	keyText = strings.TrimSpace(keyText)
	if keyText == "" {
		return nil, nil
	}
	key, err := decodeTokenEncryptionKey(keyText)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretBox{aead: aead}, nil
}

func decodeTokenEncryptionKey(keyText string) ([]byte, error) {
	decoders := []struct {
		name   string
		decode func(string) ([]byte, error)
	}{
		{name: "base64", decode: base64.StdEncoding.DecodeString},
		{name: "raw base64", decode: base64.RawStdEncoding.DecodeString},
		{name: "URL base64", decode: base64.URLEncoding.DecodeString},
		{name: "raw URL base64", decode: base64.RawURLEncoding.DecodeString},
		{name: "hex", decode: hex.DecodeString},
	}

	var decodedWrongLength []string
	for _, decoder := range decoders {
		key, err := decoder.decode(keyText)
		if err != nil {
			continue
		}
		if len(key) == 32 {
			return key, nil
		}
		decodedWrongLength = append(decodedWrongLength, fmt.Sprintf("%s decoded to %d bytes", decoder.name, len(key)))
	}
	if len(decodedWrongLength) > 0 {
		return nil, fmt.Errorf("token encryption key must decode to 32 bytes (%s)", strings.Join(decodedWrongLength, "; "))
	}
	return nil, fmt.Errorf("token encryption key must be base64 or 64-character hex for 32 bytes")
}

func requiresTokenEncryption(cfg Config) bool {
	return cfg.WebProtocol == "https" && strings.TrimSpace(cfg.PublicAPIHost) != ""
}

func (s *server) encryptSecret(value string) (string, error) {
	if value == "" || s.secretBox == nil || isEncryptedSecret(value) {
		return value, nil
	}
	nonce := make([]byte, s.secretBox.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := s.secretBox.aead.Seal(nil, nonce, []byte(value), nil)
	payload := append(nonce, ciphertext...)
	return encryptedSecretPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (s *server) decryptSecret(value string) (string, error) {
	if value == "" || !isEncryptedSecret(value) {
		return value, nil
	}
	if s.secretBox == nil {
		return "", fmt.Errorf("encrypted token requires B11K_TOKEN_ENCRYPTION_KEY")
	}
	payloadText := strings.TrimPrefix(value, encryptedSecretPrefix)
	payload, err := base64.RawURLEncoding.DecodeString(payloadText)
	if err != nil {
		if payload, err = base64.StdEncoding.DecodeString(payloadText); err != nil {
			return "", fmt.Errorf("invalid encrypted token payload")
		}
	}
	nonceSize := s.secretBox.aead.NonceSize()
	if len(payload) <= nonceSize {
		return "", fmt.Errorf("invalid encrypted token payload")
	}
	nonce := payload[:nonceSize]
	ciphertext := payload[nonceSize:]
	plaintext, err := s.secretBox.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token")
	}
	return string(plaintext), nil
}

func isEncryptedSecret(value string) bool {
	return strings.HasPrefix(value, encryptedSecretPrefix)
}
