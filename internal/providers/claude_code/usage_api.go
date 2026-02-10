package claude_code

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // already in go.mod for cursor provider

	"golang.org/x/crypto/pbkdf2"
)

type usageResponse struct {
	FiveHour          *usageBucket `json:"five_hour"`
	SevenDay          *usageBucket `json:"seven_day"`
	SevenDaySonnet    *usageBucket `json:"seven_day_sonnet"`
	SevenDayOpus      *usageBucket `json:"seven_day_opus"`
	SevenDayCowork    *usageBucket `json:"seven_day_cowork"`
	SevenDayOAuthApps *usageBucket `json:"seven_day_oauth_apps"`
	ExtraUsage        *usageBucket `json:"extra_usage"`
}

type usageBucket struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func getClaudeSessionCookies() (map[string]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("cookie extraction only supported on macOS")
	}

	encKey, err := getChromiumEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("getting encryption key: %w", err)
	}

	cookiesPath := filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Claude", "Cookies")
	if _, err := os.Stat(cookiesPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Claude desktop Cookies DB not found: %s", cookiesPath)
	}

	tmpFile, err := os.CreateTemp("", "claude-cookies-*.db")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	srcData, err := os.ReadFile(cookiesPath)
	if err != nil {
		return nil, fmt.Errorf("reading cookies DB: %w", err)
	}
	if err := os.WriteFile(tmpPath, srcData, 0600); err != nil {
		return nil, fmt.Errorf("writing temp cookies DB: %w", err)
	}

	db, err := sql.Open("sqlite3", tmpPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opening cookies DB: %w", err)
	}
	defer db.Close()

	wantCookies := []string{"sessionKey", "cf_clearance", "anthropic-device-id", "lastActiveOrg", "__cf_bm"}
	placeholders := make([]string, len(wantCookies))
	args := make([]interface{}, len(wantCookies))
	for i, name := range wantCookies {
		placeholders[i] = "?"
		args[i] = name
	}

	query := fmt.Sprintf(
		"SELECT name, encrypted_value FROM cookies WHERE host_key LIKE '%%claude.ai%%' AND name IN (%s)",
		strings.Join(placeholders, ","),
	)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying cookies: %w", err)
	}
	defer rows.Close()

	cookies := make(map[string]string)
	for rows.Next() {
		var name string
		var encValue []byte
		if err := rows.Scan(&name, &encValue); err != nil {
			continue
		}
		decrypted, err := decryptChromiumCookie(encValue, encKey)
		if err != nil {
			continue // skip cookies we can't decrypt
		}
		cookies[name] = decrypted
	}

	if _, ok := cookies["sessionKey"]; !ok {
		return nil, fmt.Errorf("sessionKey cookie not found (Claude desktop app may not be logged in)")
	}

	return cookies, nil
}

func getChromiumEncryptionKey() ([]byte, error) {
	cmd := exec.Command("security", "find-generic-password", "-w", "-s", "Claude Safe Storage", "-a", "Claude")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("keychain lookup failed (is Claude desktop installed?): %w", err)
	}
	password := strings.TrimSpace(string(out))

	key := pbkdf2.Key([]byte(password), []byte("saltysalt"), 1003, 16, sha1.New)
	return key, nil
}

func decryptChromiumCookie(encrypted []byte, key []byte) (string, error) {
	if len(encrypted) < 3 {
		return "", fmt.Errorf("encrypted value too short")
	}

	prefix := string(encrypted[:3])
	if prefix != "v10" {
		return "", fmt.Errorf("unexpected cookie encryption version: %q", prefix)
	}
	ciphertext := encrypted[3:]

	if len(ciphertext) == 0 {
		return "", fmt.Errorf("empty ciphertext after prefix")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating AES cipher: %w", err)
	}

	iv := []byte("                ") // 16 spaces

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext not aligned to block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	if len(plaintext) == 0 {
		return "", fmt.Errorf("empty plaintext")
	}
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > aes.BlockSize || padLen > len(plaintext) || padLen == 0 {
		return "", fmt.Errorf("invalid PKCS7 padding")
	}
	plaintext = plaintext[:len(plaintext)-padLen]

	const chromiumPrefixLen = 32
	if len(plaintext) <= chromiumPrefixLen {
		return "", fmt.Errorf("decrypted value too short after padding removal (len=%d)", len(plaintext))
	}
	plaintext = plaintext[chromiumPrefixLen:]

	return string(plaintext), nil
}

func fetchUsageAPI(orgUUID string, cookies map[string]string) (*usageResponse, error) {
	url := fmt.Sprintf("https://claude.ai/api/organizations/%s/usage", orgUUID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	var cookieParts []string
	for name, value := range cookies {
		cookieParts = append(cookieParts, fmt.Sprintf("%s=%s", name, value))
	}
	req.Header.Set("Cookie", strings.Join(cookieParts, "; "))

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://claude.ai/settings/usage")
	req.Header.Set("anthropic-client-platform", "web_claude_ai")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var usage usageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &usage, nil
}
