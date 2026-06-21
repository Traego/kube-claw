// Package secrets is the kube-claw secret authority: encryption, secret/version
// storage, intake, approval, and grants (DESIGN.md §8-§16).
package secrets

import (
	"bytes"
	"fmt"
	"os"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"
)

// Cipher encrypts/decrypts secret material. The associatedData binds a
// ciphertext to its secret id (a ciphertext for secret A can't be swapped in
// for secret B). The interface lets the master key move from a local dev keyset
// to a KMS-backed keyset without touching callers (DESIGN.md §12).
type Cipher interface {
	Encrypt(plaintext, associatedData []byte) ([]byte, error)
	Decrypt(ciphertext, associatedData []byte) ([]byte, error)
}

type tinkCipher struct{ aead tink.AEAD }

func (c *tinkCipher) Encrypt(pt, aad []byte) ([]byte, error) { return c.aead.Encrypt(pt, aad) }
func (c *tinkCipher) Decrypt(ct, aad []byte) ([]byte, error) { return c.aead.Decrypt(ct, aad) }

// NewLocalCipher loads an AES-256-GCM keyset from path, creating it (0600) if
// absent. This is the v0 DEV master-key path ("local key on disk", DESIGN.md
// §12). PRODUCTION must wrap the keyset with a KMS KEK instead of cleartext —
// swap this constructor for a KMS-backed one; the Cipher interface is unchanged.
func NewLocalCipher(path string) (Cipher, error) {
	var handle *keyset.Handle

	if b, err := os.ReadFile(path); err == nil {
		h, err := insecurecleartextkeyset.Read(keyset.NewBinaryReader(bytes.NewReader(b)))
		if err != nil {
			return nil, fmt.Errorf("read keyset %s: %w", path, err)
		}
		handle = h
	} else if os.IsNotExist(err) {
		h, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
		if err != nil {
			return nil, fmt.Errorf("generate keyset: %w", err)
		}
		if err := writeKeyset(h, path); err != nil {
			return nil, err
		}
		handle = h
	} else {
		return nil, fmt.Errorf("stat keyset %s: %w", path, err)
	}

	a, err := aead.New(handle)
	if err != nil {
		return nil, fmt.Errorf("aead from keyset: %w", err)
	}
	return &tinkCipher{aead: a}, nil
}

func writeKeyset(h *keyset.Handle, path string) error {
	var buf bytes.Buffer
	if err := insecurecleartextkeyset.Write(h, keyset.NewBinaryWriter(&buf)); err != nil {
		return fmt.Errorf("serialize keyset: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write keyset %s: %w", path, err)
	}
	return nil
}
