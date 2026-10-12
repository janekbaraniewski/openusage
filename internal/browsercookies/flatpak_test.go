package browsercookies

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlatpakFirefoxStoresFindsProfiles(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox", "abc.default-release")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(profile, "cookies.sqlite")
	if err := os.WriteFile(db, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	stores := flatpakFirefoxStores(home)

	if len(stores) != 1 {
		t.Fatalf("got %d stores, want 1", len(stores))
	}
	defer stores[0].Close()
	if got := canonicalBrowser(stores[0].Browser()); got != "firefox" {
		t.Errorf("Browser() = %q, want firefox", got)
	}
	if got := stores[0].FilePath(); got != db {
		t.Errorf("FilePath() = %q, want %q", got, db)
	}
}

func TestFlatpakFirefoxStoresNoInstall(t *testing.T) {
	if stores := flatpakFirefoxStores(t.TempDir()); len(stores) != 0 {
		t.Fatalf("got %d stores, want 0", len(stores))
	}
}
