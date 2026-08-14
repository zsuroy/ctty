package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/zsuroy/ctty/internal/config"
)

// Credential represents a stored password for a host.
type Credential struct {
	HostName  string    `json:"host_name"`
	Password  string    `json:"password"` // Encrypted with machine-derived AES-GCM key
	UpdatedAt time.Time `json:"updated_at"`
}

// CredentialStore manages stored credentials in ~/.config/ctty/credentials.json
type CredentialStore struct {
	mu          sync.RWMutex
	credentials map[string]Credential
	filePath    string
	masterKey   []byte
}

var (
	defaultStore *CredentialStore
	once         sync.Once
)

// getStore returns the singleton credential store.
func getStore() (*CredentialStore, error) {
	var initErr error
	once.Do(func() {
		configDir, err := config.GetcttyConfigDir()
		if err != nil {
			initErr = err
			return
		}

		filePath := filepath.Join(configDir, "credentials.json")
		key := deriveMachineKey()

		store := &CredentialStore{
			credentials: make(map[string]Credential),
			filePath:    filePath,
			masterKey:   key,
		}

		if err := store.load(); err != nil && !os.IsNotExist(err) {
			// If file exists but corrupt, keep empty store
		}

		defaultStore = store
	})

	if initErr != nil {
		return nil, initErr
	}
	return defaultStore, nil
}

// deriveMachineKey derives a stable AES-256 key from machine and user identity.
func deriveMachineKey() []byte {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	hostname, _ := os.Hostname()
	goos := runtime.GOOS

	seed := "ctty-secret-key-" + user + "@" + hostname + "-" + goos
	hash := sha256.Sum256([]byte(seed))
	return hash[:]
}

// encrypt encrypts plaintext using AES-256-GCM.
func encrypt(plaintext string, key []byte) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts base64-encoded ciphertext using AES-256-GCM.
func decrypt(ciphertextBase64 string, key []byte) (string, error) {
	if ciphertextBase64 == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("malformed ciphertext")
	}
	nonce := data[:gcm.NonceSize()]
	ciphertext := data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// load reads the credentials file from disk.
func (s *CredentialStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var list []Credential
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}

	s.credentials = make(map[string]Credential, len(list))
	for _, c := range list {
		s.credentials[c.HostName] = c
	}
	return nil
}

// save writes the credentials file to disk with 0600 permissions.
func (s *CredentialStore) save() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	list := make([]Credential, 0, len(s.credentials))
	for _, c := range s.credentials {
		list = append(list, c)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0600)
}

// GetPassword retrieves the decrypted password for a host.
func GetPassword(hostName string) (string, bool) {
	store, err := getStore()
	if err != nil {
		return "", false
	}

	store.mu.RLock()
	cred, exists := store.credentials[hostName]
	store.mu.RUnlock()

	if !exists || cred.Password == "" {
		return "", false
	}

	plain, err := decrypt(cred.Password, store.masterKey)
	if err != nil {
		return "", false
	}
	return plain, true
}

// SetPassword saves an encrypted password for a host.
func SetPassword(hostName, password string) error {
	if hostName == "" {
		return errors.New("host name cannot be empty")
	}

	store, err := getStore()
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if password == "" {
		delete(store.credentials, hostName)
		return store.save()
	}

	encrypted, err := encrypt(password, store.masterKey)
	if err != nil {
		return err
	}

	store.credentials[hostName] = Credential{
		HostName:  hostName,
		Password:  encrypted,
		UpdatedAt: time.Now(),
	}
	return store.save()
}

// DeletePassword removes a stored password for a host.
func DeletePassword(hostName string) error {
	store, err := getStore()
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if _, exists := store.credentials[hostName]; exists {
		delete(store.credentials, hostName)
		return store.save()
	}
	return nil
}

// RenameHost updates the stored credential when a host is renamed.
func RenameHost(oldName, newName string) error {
	if oldName == newName || oldName == "" || newName == "" {
		return nil
	}

	store, err := getStore()
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if cred, exists := store.credentials[oldName]; exists {
		cred.HostName = newName
		cred.UpdatedAt = time.Now()
		store.credentials[newName] = cred
		delete(store.credentials, oldName)
		return store.save()
	}
	return nil
}
