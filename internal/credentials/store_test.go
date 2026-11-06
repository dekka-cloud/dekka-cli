package credentials

import (
	"errors"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

const api = "https://api.example.test"

func TestKeychainRoundTrip(t *testing.T) {
	keyring.MockInit()
	s := Store{Dir: t.TempDir()}

	saved, err := s.Set(api, "dk_abc_secret")
	if err != nil || saved.Source != SourceKeychain {
		t.Fatalf("Set = %+v, %v", saved, err)
	}
	key, src, err := s.Get(api)
	if err != nil || key != "dk_abc_secret" || src != SourceKeychain {
		t.Fatalf("Get = %q, %q, %v", key, src, err)
	}
	if _, _, err := s.Get("https://other.example.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a key for one API URL was returned for another: %v", err)
	}
	removed, err := s.Delete(api)
	if err != nil || !removed {
		t.Fatalf("Delete = %v, %v", removed, err)
	}
	if _, _, err := s.Get(api); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
}

func TestFileFallbackWhenKeychainIsUnavailable(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	s := Store{Dir: t.TempDir()}

	saved, err := s.Set(api, "dk_abc_secret")
	if err != nil || saved.Source != SourceFile || saved.KeychainErr == nil {
		t.Fatalf("Set = %+v, %v", saved, err)
	}
	path, _ := s.Path()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("credentials file mode = %o, want 600", perm)
	}
	key, src, err := s.Get(api)
	if err != nil || key != "dk_abc_secret" || src != SourceFile {
		t.Fatalf("Get = %q, %q, %v", key, src, err)
	}
	removed, err := s.Delete(api)
	if err != nil || !removed {
		t.Fatalf("Delete = %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the empty credentials file was left behind: %v", err)
	}
}

func TestKeychainSaveClearsAnOlderFileKey(t *testing.T) {
	dir := t.TempDir()
	keyring.MockInitWithError(errors.New("locked"))
	if _, err := (Store{Dir: dir}).Set(api, "dk_old_secret"); err != nil {
		t.Fatal(err)
	}
	keyring.MockInit()
	s := Store{Dir: dir}
	if _, err := s.Set(api, "dk_new_secret"); err != nil {
		t.Fatal(err)
	}
	keys, err := s.readFile()
	if err != nil || len(keys) != 0 {
		t.Fatalf("file still holds %v (%v)", keys, err)
	}
}
