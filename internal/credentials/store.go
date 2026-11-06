// Package credentials keeps the API key the CLI signs requests with. Keys
// live in the operating system's keychain, and when no keychain is available
// (a headless Linux machine without a Secret Service, for example) in a file
// under the user's config directory that only the user can read.
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

// Service is the keychain service name the CLI stores keys under. Each key is
// stored against the API URL it was verified with, so a key for one API is
// never sent to another.
const Service = "dekka-cli"

// Source says where a key was found.
type Source string

const (
	SourceEnv      Source = "the DEKKA_TOKEN environment variable"
	SourceKeychain Source = "the system keychain"
	SourceFile     Source = "the credentials file"
)

// ErrNotFound means no key is stored for the API URL.
var ErrNotFound = errors.New("no key stored")

// Store reads and writes keys. Dir is where the fallback file lives; it
// defaults to <user config dir>/dekka.
type Store struct {
	Dir string
}

// Path is the fallback credentials file.
func (s Store) Path() (string, error) {
	dir := s.Dir
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "dekka")
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// Get returns the key stored for apiURL, looking in the keychain first.
func (s Store) Get(apiURL string) (string, Source, error) {
	key, err := keyring.Get(Service, apiURL)
	if err == nil && key != "" {
		return key, SourceKeychain, nil
	}
	keys, err := s.readFile()
	if err != nil {
		return "", "", err
	}
	if key, ok := keys[apiURL]; ok && key != "" {
		return key, SourceFile, nil
	}
	return "", "", ErrNotFound
}

// Saved says where Set put a key. KeychainErr is why the keychain refused it
// when the file was used instead.
type Saved struct {
	Source      Source
	KeychainErr error
}

// Set stores key for apiURL, in the keychain when it accepts the key and in
// the file otherwise.
func (s Store) Set(apiURL, key string) (Saved, error) {
	kerr := keyring.Set(Service, apiURL, key)
	if kerr == nil {
		// A key left in the file by an earlier fallback would otherwise outlive this one.
		return Saved{Source: SourceKeychain}, s.deleteFromFile(apiURL)
	}
	keys, err := s.readFile()
	if err != nil {
		return Saved{}, err
	}
	keys[apiURL] = key
	if err := s.writeFile(keys); err != nil {
		return Saved{}, err
	}
	return Saved{Source: SourceFile, KeychainErr: kerr}, nil
}

// Delete removes the key for apiURL from the keychain and the file, and
// reports whether there was one to remove. A machine without a keychain has
// nothing stored there, so a keychain error only matters for the file.
func (s Store) Delete(apiURL string) (bool, error) {
	removed := keyring.Delete(Service, apiURL) == nil
	keys, err := s.readFile()
	if err != nil {
		return removed, err
	}
	if _, ok := keys[apiURL]; !ok {
		return removed, nil
	}
	return true, s.deleteFromFile(apiURL)
}

func (s Store) readFile() (map[string]string, error) {
	path, err := s.Path()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	keys := map[string]string{}
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	return keys, nil
}

func (s Store) writeFile(keys map[string]string) error {
	path, err := s.Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (s Store) deleteFromFile(apiURL string) error {
	keys, err := s.readFile()
	if err != nil {
		return err
	}
	if _, ok := keys[apiURL]; !ok {
		return nil
	}
	delete(keys, apiURL)
	if len(keys) == 0 {
		path, err := s.Path()
		if err != nil {
			return err
		}
		return os.Remove(path)
	}
	return s.writeFile(keys)
}
