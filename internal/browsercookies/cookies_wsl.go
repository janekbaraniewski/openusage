package browsercookies

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func (r *kookyReader) readCookieWSL(ctx context.Context, domain, name, browser string) (Cookie, error) {
	home, _ := os.UserHomeDir()
	if home == "" {
		return Cookie{}, ErrNoCookieFound
	}
	keyFile := filepath.Join(home, ".config", "openusage", "chrome_key")
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return Cookie{}, ErrNoCookieFound
	}
	masterKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil || len(masterKey) != 32 {
		return Cookie{}, ErrNoCookieFound
	}

	usersDir := "/mnt/c/Users"
	if _, err := os.Stat(usersDir); err != nil {
		return Cookie{}, ErrNoCookieFound
	}

	entries, err := os.ReadDir(usersDir)
	if err != nil {
		return Cookie{}, ErrNoCookieFound
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "Default" || entry.Name() == "Public" {
			continue
		}
		cookiePaths := []string{
			filepath.Join(usersDir, entry.Name(), "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Network", "Cookies"),
			filepath.Join(usersDir, entry.Name(), "AppData", "Local", "Microsoft", "Edge", "User Data", "Default", "Network", "Cookies"),
		}
		for _, dbPath := range cookiePaths {
			if _, err := os.Stat(dbPath); err != nil {
				continue
			}
			tmpFile, err := os.CreateTemp("", "openusage_cookie_*.db")
			if err != nil {
				continue
			}
			tmpPath := tmpFile.Name()
			tmpFile.Close()
			defer os.Remove(tmpPath)

			src, err := os.Open(dbPath)
			if err != nil {
				continue
			}
			dst, err := os.Create(tmpPath)
			if err != nil {
				src.Close()
				continue
			}
			_, _ = io.Copy(dst, src)
			src.Close()
			dst.Close()

			db, err := sql.Open("sqlite3", tmpPath)
			if err != nil {
				continue
			}

			query := "SELECT name, host_key, encrypted_value FROM cookies WHERE host_key LIKE ? AND (name = ? OR ? = '')"
			rows, err := db.QueryContext(ctx, query, "%"+domain+"%", name, name)
			if err != nil {
				db.Close()
				continue
			}

			for rows.Next() {
				var cName, cHost string
				var encVal []byte
				if err := rows.Scan(&cName, &cHost, &encVal); err != nil {
					continue
				}
				if len(encVal) < 31 {
					continue
				}
				if string(encVal[:3]) != "v10" && string(encVal[:3]) != "v11" {
					continue
				}
				iv := encVal[3:15]
				ciphertextAndTag := encVal[15:]
				block, err := aes.NewCipher(masterKey)
				if err != nil {
					continue
				}
				aesgcm, err := cipher.NewGCM(block)
				if err != nil {
					continue
				}
				plain, err := aesgcm.Open(nil, iv, ciphertextAndTag, nil)
				if err != nil {
					continue
				}
				if len(plain) > 0 {
					rows.Close()
					db.Close()
					return Cookie{
						Name:   cName,
						Value:  string(plain),
						Domain: cHost,
						Source: "chrome-wsl",
					}, nil
				}
			}
			rows.Close()
			db.Close()
		}
	}
	return Cookie{}, ErrNoCookieFound
}
