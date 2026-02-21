package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Credentials struct {
	Keys map[string]string `json:"keys"` // account ID → API key
}

// credMu guards read-modify-write cycles on the credentials file.
var credMu sync.Mutex

func CredentialsPath() string {
	return filepath.Join(ConfigDir(), "credentials.json")
}

func LoadCredentials() (Credentials, error) {
	return LoadCredentialsFrom(CredentialsPath())
}

func LoadCredentialsFrom(path string) (Credentials, error) {
	creds := Credentials{Keys: make(map[string]string)}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return creds, nil
		}
		return creds, fmt.Errorf("reading credentials: %w", err)
	}

	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{Keys: make(map[string]string)}, fmt.Errorf("parsing credentials %s: %w", path, err)
	}

	if creds.Keys == nil {
		creds.Keys = make(map[string]string)
	}

	return creds, nil
}

func SaveCredential(accountID, apiKey string) error {
	return SaveCredentialTo(CredentialsPath(), accountID, apiKey)
}

func SaveCredentialTo(path, accountID, apiKey string) error {
	credMu.Lock()
	defer credMu.Unlock()

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		creds = Credentials{Keys: make(map[string]string)}
	}

	creds.Keys[accountID] = apiKey

	return writeCredentials(path, creds)
}

func DeleteCredential(accountID string) error {
	return DeleteCredentialFrom(CredentialsPath(), accountID)
}

func DeleteCredentialFrom(path, accountID string) error {
	credMu.Lock()
	defer credMu.Unlock()

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		return err
	}

	delete(creds.Keys, accountID)

	return writeCredentials(path, creds)
}

func writeCredentials(path string, creds Credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating credentials dir: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	return nil
}
