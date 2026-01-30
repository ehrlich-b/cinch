package crypto

import "testing"

func TestEncryptDecrypt(t *testing.T) {
	cipher, err := NewCipher("test-secret-key")
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"simple", "hello world"},
		{"json", `{"access_token":"ghs_xxx","refresh_token":"ghr_yyy"}`},
		{"unicode", "🔒 encrypted 🔒"},
		{"long", "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
			"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := cipher.Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			// Empty strings should not be encrypted
			if tt.plaintext == "" {
				if encrypted != "" {
					t.Errorf("empty string should stay empty, got %q", encrypted)
				}
				return
			}

			// Encrypted value should have prefix
			if !IsEncrypted(encrypted) {
				t.Errorf("encrypted value should have enc: prefix")
			}

			// Decrypt should return original
			decrypted, err := cipher.Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}
			if decrypted != tt.plaintext {
				t.Errorf("decrypted = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestPlaintextPassthrough(t *testing.T) {
	cipher, err := NewCipher("test-secret-key")
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}

	// Plaintext without enc: prefix should pass through unchanged
	plaintext := "not-encrypted-value"
	result, err := cipher.Decrypt(plaintext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if result != plaintext {
		t.Errorf("plaintext passthrough failed: got %q, want %q", result, plaintext)
	}
}

func TestDoubleEncryptIsNoop(t *testing.T) {
	cipher, err := NewCipher("test-secret-key")
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}

	plaintext := "secret-value"
	encrypted1, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("first Encrypt failed: %v", err)
	}

	// Encrypting again should return the same value (already encrypted)
	encrypted2, err := cipher.Encrypt(encrypted1)
	if err != nil {
		t.Fatalf("second Encrypt failed: %v", err)
	}

	if encrypted1 != encrypted2 {
		t.Errorf("double encrypt changed value: %q != %q", encrypted1, encrypted2)
	}
}

func TestDifferentKeysProduceDifferentCiphertext(t *testing.T) {
	cipher1, _ := NewCipher("key1")
	cipher2, _ := NewCipher("key2")

	plaintext := "secret"
	enc1, _ := cipher1.Encrypt(plaintext)
	enc2, _ := cipher2.Encrypt(plaintext)

	// Different keys should produce different ciphertext
	if enc1 == enc2 {
		t.Error("different keys produced same ciphertext")
	}

	// Each cipher should only decrypt its own ciphertext
	dec1, err := cipher1.Decrypt(enc1)
	if err != nil || dec1 != plaintext {
		t.Errorf("cipher1 failed to decrypt its own ciphertext")
	}

	// cipher2 trying to decrypt cipher1's ciphertext should fail
	_, err = cipher2.Decrypt(enc1)
	if err == nil {
		t.Error("cipher2 should not decrypt cipher1's ciphertext")
	}
}

func TestEmptySecretFails(t *testing.T) {
	_, err := NewCipher("")
	if err == nil {
		t.Error("expected error for empty secret")
	}
}
