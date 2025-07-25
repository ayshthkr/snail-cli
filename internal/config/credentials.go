package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
)

// CredentialManager handles secure credential storage and retrieval
type CredentialManager struct {
	service string
	storage string
}

// Credentials represents stored credentials
type Credentials struct {
	Username     string `json:"username"`
	Password     string `json:"password,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

// NewCredentialManager creates a new credential manager
func NewCredentialManager(service, storage string) *CredentialManager {
	if service == "" {
		service = "snail-cli"
	}
	if storage == "" {
		storage = "keychain"
	}
	
	return &CredentialManager{
		service: service,
		storage: storage,
	}
}

// Store stores credentials securely
func (cm *CredentialManager) Store(username string, creds *Credentials) error {
	// Serialize credentials
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("failed to serialize credentials: %w", err)
	}

	switch cm.storage {
	case "keychain":
		return cm.storeInKeychain(username, string(data))
	case "file":
		return cm.storeInFile(username, data)
	default:
		return fmt.Errorf("unsupported credential storage: %s", cm.storage)
	}
}

// Retrieve retrieves credentials securely
func (cm *CredentialManager) Retrieve(username string) (*Credentials, error) {
	var data []byte
	var err error

	switch cm.storage {
	case "keychain":
		dataStr, err := cm.retrieveFromKeychain(username)
		if err != nil {
			return nil, err
		}
		data = []byte(dataStr)
	case "file":
		data, err = cm.retrieveFromFile(username)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported credential storage: %s", cm.storage)
	}

	// Deserialize credentials
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to deserialize credentials: %w", err)
	}

	return &creds, nil
}

// Delete removes stored credentials
func (cm *CredentialManager) Delete(username string) error {
	switch cm.storage {
	case "keychain":
		return cm.deleteFromKeychain(username)
	case "file":
		return cm.deleteFromFile(username)
	default:
		return fmt.Errorf("unsupported credential storage: %s", cm.storage)
	}
}

// List returns a list of stored usernames
func (cm *CredentialManager) List() ([]string, error) {
	switch cm.storage {
	case "keychain":
		// Keychain doesn't support listing, so we maintain a separate index
		return cm.listFromKeychainIndex()
	case "file":
		return cm.listFromFiles()
	default:
		return nil, fmt.Errorf("unsupported credential storage: %s", cm.storage)
	}
}

// storeInKeychain stores credentials in the system keychain
func (cm *CredentialManager) storeInKeychain(username, data string) error {
	if !cm.isKeychainSupported() {
		return fmt.Errorf("keychain storage not supported on this platform")
	}

	// Store the credentials
	if err := keyring.Set(cm.service, username, data); err != nil {
		return fmt.Errorf("failed to store credentials in keychain: %w", err)
	}

	// Update the index of stored usernames
	if err := cm.updateKeychainIndex(username, true); err != nil {
		// Log warning but don't fail the operation
		fmt.Fprintf(os.Stderr, "Warning: failed to update keychain index: %v\n", err)
	}

	return nil
}

// retrieveFromKeychain retrieves credentials from the system keychain
func (cm *CredentialManager) retrieveFromKeychain(username string) (string, error) {
	if !cm.isKeychainSupported() {
		return "", fmt.Errorf("keychain storage not supported on this platform")
	}

	data, err := keyring.Get(cm.service, username)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve credentials from keychain: %w", err)
	}

	return data, nil
}

// deleteFromKeychain removes credentials from the system keychain
func (cm *CredentialManager) deleteFromKeychain(username string) error {
	if !cm.isKeychainSupported() {
		return fmt.Errorf("keychain storage not supported on this platform")
	}

	if err := keyring.Delete(cm.service, username); err != nil {
		return fmt.Errorf("failed to delete credentials from keychain: %w", err)
	}

	// Update the index
	if err := cm.updateKeychainIndex(username, false); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update keychain index: %v\n", err)
	}

	return nil
}

// storeInFile stores encrypted credentials in a file
func (cm *CredentialManager) storeInFile(username string, data []byte) error {
	credDir := cm.getCredentialDir()
	if err := os.MkdirAll(credDir, 0700); err != nil {
		return fmt.Errorf("failed to create credential directory: %w", err)
	}

	// Generate encryption key from username and service
	key := cm.generateEncryptionKey(username)

	// Encrypt the data
	encryptedData, err := cm.encrypt(data, key)
	if err != nil {
		return fmt.Errorf("failed to encrypt credentials: %w", err)
	}

	// Write to file
	credFile := filepath.Join(credDir, fmt.Sprintf("%s.cred", username))
	if err := os.WriteFile(credFile, encryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write credential file: %w", err)
	}

	return nil
}

// retrieveFromFile retrieves and decrypts credentials from a file
func (cm *CredentialManager) retrieveFromFile(username string) ([]byte, error) {
	credFile := filepath.Join(cm.getCredentialDir(), fmt.Sprintf("%s.cred", username))
	
	encryptedData, err := os.ReadFile(credFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read credential file: %w", err)
	}

	// Generate decryption key
	key := cm.generateEncryptionKey(username)

	// Decrypt the data
	data, err := cm.decrypt(encryptedData, key)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}

	return data, nil
}

// deleteFromFile removes credential file
func (cm *CredentialManager) deleteFromFile(username string) error {
	credFile := filepath.Join(cm.getCredentialDir(), fmt.Sprintf("%s.cred", username))
	
	if err := os.Remove(credFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete credential file: %w", err)
	}

	return nil
}

// listFromFiles lists credential files
func (cm *CredentialManager) listFromFiles() ([]string, error) {
	credDir := cm.getCredentialDir()
	
	entries, err := os.ReadDir(credDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read credential directory: %w", err)
	}

	var usernames []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".cred" {
			username := strings.TrimSuffix(entry.Name(), ".cred")
			usernames = append(usernames, username)
		}
	}

	return usernames, nil
}

// isKeychainSupported checks if keychain storage is supported on the current platform
func (cm *CredentialManager) isKeychainSupported() bool {
	switch runtime.GOOS {
	case "darwin", "windows", "linux":
		return true
	default:
		return false
	}
}

// getCredentialDir returns the directory for storing credential files
func (cm *CredentialManager) getCredentialDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ConfigDirName, "credentials")
}

// generateEncryptionKey generates an encryption key from username and service
func (cm *CredentialManager) generateEncryptionKey(username string) []byte {
	h := sha256.New()
	h.Write([]byte(cm.service))
	h.Write([]byte(username))
	return h.Sum(nil)
}

// encrypt encrypts data using AES-GCM
func (cm *CredentialManager) encrypt(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// decrypt decrypts data using AES-GCM
func (cm *CredentialManager) decrypt(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// updateKeychainIndex maintains an index of stored usernames for keychain storage
func (cm *CredentialManager) updateKeychainIndex(username string, add bool) error {
	indexKey := fmt.Sprintf("%s-index", cm.service)
	
	// Get current index
	var usernames []string
	if indexData, err := keyring.Get(cm.service, indexKey); err == nil {
		if err := json.Unmarshal([]byte(indexData), &usernames); err != nil {
			// If we can't parse the index, start fresh
			usernames = []string{}
		}
	}

	// Update index
	if add {
		// Add username if not already present
		found := false
		for _, u := range usernames {
			if u == username {
				found = true
				break
			}
		}
		if !found {
			usernames = append(usernames, username)
		}
	} else {
		// Remove username
		for i, u := range usernames {
			if u == username {
				usernames = append(usernames[:i], usernames[i+1:]...)
				break
			}
		}
	}

	// Store updated index
	indexData, err := json.Marshal(usernames)
	if err != nil {
		return err
	}

	return keyring.Set(cm.service, indexKey, string(indexData))
}

// listFromKeychainIndex retrieves the list of usernames from the keychain index
func (cm *CredentialManager) listFromKeychainIndex() ([]string, error) {
	indexKey := fmt.Sprintf("%s-index", cm.service)
	
	indexData, err := keyring.Get(cm.service, indexKey)
	if err != nil {
		// If index doesn't exist, return empty list
		return []string{}, nil
	}

	var usernames []string
	if err := json.Unmarshal([]byte(indexData), &usernames); err != nil {
		return nil, fmt.Errorf("failed to parse keychain index: %w", err)
	}

	return usernames, nil
}