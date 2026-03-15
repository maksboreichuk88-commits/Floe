// Package vault implements AES-256-GCM encrypted storage for API keys.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/floe-dev/floe/internal/config"
)

// Keyring manages the encrypted storage of secrets.
type Keyring struct {
	mu         sync.RWMutex
	path       string
	masterKey  []byte
	secrets    map[string]string
	isUnlocked bool
}

// NewKeyring initializes a new keyring at the configured path.
func NewKeyring(cfg config.VaultConfig) *Keyring {
	return &Keyring{
		path:    cfg.Path,
		secrets: make(map[string]string),
	}
}

// Unlock reads the encrypted vault and decrypts it into memory using the master key.
func (k *Keyring) Unlock(masterKey []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if len(masterKey) != 32 {
		return errors.New("master key must be exactly 32 bytes (256-bit)")
	}

	k.masterKey = make([]byte, 32)
	copy(k.masterKey, masterKey)

	data, err := os.ReadFile(k.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize an empty unlocked vault if it doesn't exist
			k.isUnlocked = true
			return nil
		}
		return fmt.Errorf("reading vault file: %w", err)
	}

	block, err := aes.NewCipher(k.masterKey)
	if err != nil {
		return fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("creating GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return errors.New("failed to decrypt vault (incorrect master key or corrupted data)")
	}

	if err := json.Unmarshal(plaintext, &k.secrets); err != nil {
		return fmt.Errorf("parsing vault data: %w", err)
	}

	k.isUnlocked = true
	return nil
}

// Get retrieves a decrypted secret from the vault.
func (k *Keyring) Get(key string) (string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	if !k.isUnlocked {
		return "", errors.New("vault is locked")
	}

	val, ok := k.secrets[key]
	if !ok {
		return "", fmt.Errorf("secret %q not found in vault", key)
	}
	return val, nil
}

// Put adds or updates a secret in the vault and persists it securely.
func (k *Keyring) Put(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.isUnlocked {
		return errors.New("vault is locked")
	}

	k.secrets[key] = value
	return k.save()
}

// Delete removes a secret from the vault.
func (k *Keyring) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.isUnlocked {
		return errors.New("vault is locked")
	}

	delete(k.secrets, key)
	return k.save()
}

// IsUnlocked returns true if the vault is currently unlocked.
func (k *Keyring) IsUnlocked() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.isUnlocked
}

// save encrypts the current secrets and writes them to disk.
// Caller must hold k.mu Lock.
func (k *Keyring) save() error {
	if err := os.MkdirAll(filepath.Dir(k.path), 0700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	plaintext, err := json.Marshal(k.secrets)
	if err != nil {
		return fmt.Errorf("marshaling secrets: %w", err)
	}

	block, err := aes.NewCipher(k.masterKey)
	if err != nil {
		return fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Write securely with narrow permissions (0600)
	return os.WriteFile(k.path, ciphertext, 0600)
}

// GenerateMasterKey creates a new cryptographic random 32-byte key.
func GenerateMasterKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}
