package browsercookies

import (
	"context"
	"os"
	"path/filepath"

	"github.com/browserutils/kooky"
	"github.com/browserutils/kooky/browser/firefox"
)

// flatpakFirefoxGlobs are the cookie databases of Firefox installed as a
// Flatpak, relative to the user's home directory. kooky's finders only look
// in the native ~/.mozilla location, so on distributions where browsers ship
// as Flatpaks (Fedora Atomic, Silverblue) they find nothing.
var flatpakFirefoxGlobs = []string{
	".var/app/org.mozilla.firefox/.mozilla/firefox/*/cookies.sqlite",
	".var/app/org.mozilla.firefox/config/mozilla/firefox/*/cookies.sqlite",
}

// findAllCookieStores returns kooky's discovered stores plus the Flatpak
// Firefox profiles kooky does not know about.
func findAllCookieStores(ctx context.Context) []kooky.CookieStore {
	stores := kooky.FindAllCookieStores(ctx)
	home, err := os.UserHomeDir()
	if err != nil {
		return stores
	}
	return append(stores, flatpakFirefoxStores(home)...)
}

func flatpakFirefoxStores(home string) []kooky.CookieStore {
	var out []kooky.CookieStore
	for _, pattern := range flatpakFirefoxGlobs {
		paths, err := filepath.Glob(filepath.Join(home, pattern))
		if err != nil {
			continue
		}
		for _, path := range paths {
			store, err := firefox.CookieStore(path)
			if err != nil || store == nil {
				continue
			}
			out = append(out, store)
		}
	}
	return out
}
