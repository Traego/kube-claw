package secrets

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestLocalCipher_RoundTripAndPersistence(t *testing.T) {
	ksPath := filepath.Join(t.TempDir(), "master.keyset")

	c1, err := NewLocalCipher(ksPath)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	plaintext := []byte(`{"type":"service_account","private_key":"..."}`)
	aad := []byte("secret-id-123")

	ct, err := c1.Encrypt(plaintext, aad)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Contains(ct, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}

	// A second cipher loading the SAME keyset file must decrypt (persistence).
	c2, err := NewLocalCipher(ksPath)
	if err != nil {
		t.Fatalf("reload cipher: %v", err)
	}
	got, err := c2.Decrypt(ct, aad)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("decrypt mismatch")
	}

	// Wrong associated data (different secret id) must fail.
	if _, err := c2.Decrypt(ct, []byte("other-secret")); err == nil {
		t.Fatal("decrypt succeeded with wrong associated data")
	}

	// A different keyset must NOT decrypt (wrong master key).
	other, err := NewLocalCipher(filepath.Join(t.TempDir(), "other.keyset"))
	if err != nil {
		t.Fatalf("other cipher: %v", err)
	}
	if _, err := other.Decrypt(ct, aad); err == nil {
		t.Fatal("decrypt succeeded with wrong master key")
	}
}
