package credential

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := deriveMachineKey()
	original := "superSecretP@ssw0rd!123"

	encrypted, err := encrypt(original, key)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	if encrypted == original {
		t.Fatal("Encrypted text should not match original")
	}

	decrypted, err := decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if decrypted != original {
		t.Fatalf("Expected %s, got %s", original, decrypted)
	}
}

func TestCredentialStoreOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ctty-cred-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "credentials.json")
	key := deriveMachineKey()

	store := &CredentialStore{
		credentials: make(map[string]Credential),
		filePath:    filePath,
		masterKey:   key,
	}

	defaultStore = store

	// 1. Set password
	err = SetPassword("test-host", "mypassword123")
	if err != nil {
		t.Fatalf("Failed to set password: %v", err)
	}

	// 2. Get password
	pass, found := GetPassword("test-host")
	if !found {
		t.Fatal("Expected password to be found")
	}
	if pass != "mypassword123" {
		t.Fatalf("Expected mypassword123, got %s", pass)
	}

	// 3. Rename host
	err = RenameHost("test-host", "test-host-renamed")
	if err != nil {
		t.Fatalf("Failed to rename host: %v", err)
	}

	_, found = GetPassword("test-host")
	if found {
		t.Fatal("Old host name should not have password")
	}

	pass, found = GetPassword("test-host-renamed")
	if !found || pass != "mypassword123" {
		t.Fatalf("Expected renamed host to have password, got %s (found: %v)", pass, found)
	}

	// 4. Delete password
	err = DeletePassword("test-host-renamed")
	if err != nil {
		t.Fatalf("Failed to delete password: %v", err)
	}

	_, found = GetPassword("test-host-renamed")
	if found {
		t.Fatal("Deleted password should not be found")
	}
}
