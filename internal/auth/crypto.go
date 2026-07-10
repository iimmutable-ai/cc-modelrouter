package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// masterKeyOnce ensures the master key is loaded only once.
var masterKeyOnce sync.Once
var masterKeyBytes []byte
var masterKeyErr error

// masterKeyPath returns the path to the master key file.
func masterKeyPath() (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "master.key"), nil
}

// loadMasterKey loads or generates the 256-bit master key.
func loadMasterKey() ([]byte, error) {
	masterKeyOnce.Do(func() {
		path, err := masterKeyPath()
		if err != nil {
			masterKeyErr = err
			return
		}

		// Try to read existing key
		data, err := os.ReadFile(path)
		if err == nil && len(data) == 32 {
			masterKeyBytes = data
			return
		}

		// Generate new key
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			masterKeyErr = fmt.Errorf("failed to generate master key: %w", err)
			return
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			masterKeyErr = fmt.Errorf("failed to create config directory: %w", err)
			return
		}

		// Write key file with restrictive permissions
		if err := os.WriteFile(path, key, 0600); err != nil {
			masterKeyErr = fmt.Errorf("failed to write master key: %w", err)
			return
		}

		masterKeyBytes = key
	})

	return masterKeyBytes, masterKeyErr
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns base64-encoded ciphertext (nonce + ciphertext + tag).
func Encrypt(plaintext string) (string, error) {
	key, err := loadMasterKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded AES-256-GCM ciphertext.
func Decrypt(encoded string) (string, error) {
	key, err := loadMasterKey()
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
