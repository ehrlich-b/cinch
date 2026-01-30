package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

const encryptedPrefix = "enc:"

// Cipher handles AES-256-GCM encryption and decryption.
type Cipher struct {
	gcm cipher.AEAD
}

// NewCipher creates a new cipher from a secret key.
// The key is hashed with SHA-256 to ensure it's exactly 32 bytes for AES-256.
func NewCipher(secret string) (*Cipher, error) {
	if secret == "" {
		return nil, errors.New("encryption secret cannot be empty")
	}

	// Hash the secret to get exactly 32 bytes for AES-256
	hash := sha256.Sum256([]byte(secret))

	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &Cipher{gcm: gcm}, nil
}

// Encrypt encrypts plaintext and returns a prefixed base64-encoded ciphertext.
// Empty strings are returned as-is (no encryption needed for empty values).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	// Already encrypted? Return as-is.
	if strings.HasPrefix(plaintext, encryptedPrefix) {
		return plaintext, nil
	}

	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := c.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a prefixed base64-encoded ciphertext.
// If the value is not encrypted (no prefix), it's returned as-is (plaintext passthrough for migration).
func (c *Cipher) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Not encrypted? Return as-is (plaintext passthrough for migration).
	if !strings.HasPrefix(ciphertext, encryptedPrefix) {
		return ciphertext, nil
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, encryptedPrefix))
	if err != nil {
		return "", err
	}

	nonceSize := c.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := c.gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// IsEncrypted returns true if the value appears to be encrypted.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encryptedPrefix)
}
