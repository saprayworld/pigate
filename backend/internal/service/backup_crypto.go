package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"

	"pigate/internal/model"
)

// Argon2id parameters for deriving the AES-256 key from a passphrase. These are
// stored in each encrypted backup's meta so decryption uses the same values even
// if the defaults change later.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32 // AES-256
	saltLen      = 16
)

// ErrPassphraseRequired is returned when an encrypted backup is imported without
// a passphrase, so the API layer can prompt for one specifically.
var ErrPassphraseRequired = errors.New("this backup is encrypted; a passphrase is required to import it")

func deriveKey(passphrase string, salt []byte, time, memory uint32, threads uint8) []byte {
	return argon2.IDKey([]byte(passphrase), salt, time, memory, threads, argonKeyLen)
}

// encryptConfig marshals cfg and encrypts it with AES-256-GCM using an
// Argon2id-derived key. It returns the base64 ciphertext and the KDF/cipher
// parameters (salt, nonce, argon2 params) needed to decrypt it. The passphrase
// itself is never persisted.
func encryptConfig(cfg model.BackupConfig, passphrase string) (string, *model.EncryptionParams, error) {
	plaintext, err := json.Marshal(cfg)
	if err != nil {
		return "", nil, err
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", nil, fmt.Errorf("generate salt: %w", err)
	}
	key := deriveKey(passphrase, salt, argonTime, argonMemory, argonThreads)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	params := &model.EncryptionParams{
		Algorithm: "AES-256-GCM",
		KDF:       "argon2id",
		Salt:      base64.StdEncoding.EncodeToString(salt),
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
		Time:      argonTime,
		Memory:    argonMemory,
		Threads:   argonThreads,
	}
	return base64.StdEncoding.EncodeToString(ciphertext), params, nil
}

// decryptConfig reverses encryptConfig. A wrong passphrase (or any tampering)
// fails GCM authentication and returns a generic error — the caller must not
// distinguish the two to avoid a padding/passphrase oracle.
func decryptConfig(encoded, passphrase string, params *model.EncryptionParams) ([]byte, error) {
	if params == nil {
		return nil, errors.New("encrypted backup is missing its encryption parameters")
	}
	if params.Algorithm != "" && params.Algorithm != "AES-256-GCM" {
		return nil, fmt.Errorf("unsupported encryption algorithm %q", params.Algorithm)
	}
	if params.KDF != "" && params.KDF != "argon2id" {
		return nil, fmt.Errorf("unsupported key derivation %q", params.KDF)
	}

	salt, err := base64.StdEncoding.DecodeString(params.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid salt encoding: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(params.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce encoding: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext encoding: %w", err)
	}

	// Use the stored parameters, falling back to defaults if a field is absent.
	t, m, th := params.Time, params.Memory, params.Threads
	if t == 0 {
		t = argonTime
	}
	if m == 0 {
		m = argonMemory
	}
	if th == 0 {
		th = argonThreads
	}
	key := deriveKey(passphrase, salt, t, m, th)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce length")
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: wrong passphrase or corrupted file")
	}
	return plaintext, nil
}
