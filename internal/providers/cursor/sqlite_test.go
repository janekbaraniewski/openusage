package cursor

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestMain(m *testing.M) {
	// Do not read the developer's real Cursor state.vscdb during tests.
	if os.Getenv("OPENUSAGE_CURSOR_STATE_DB") == "" {
		_ = os.Setenv("OPENUSAGE_CURSOR_STATE_DB", filepath.Join(os.TempDir(), "openusage-cursor-tests-missing.vscdb"))
	}
	os.Exit(m.Run())
}

func writeTestStateDB(t *testing.T, token, email, membership string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO ItemTable (key, value) VALUES
		('cursorAuth/accessToken', ?),
		('cursorAuth/cachedEmail', ?),
		('cursorAuth/stripeMembershipType', ?);
	`, token, email, membership); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func TestExtractLocalAuth_FromSQLite(t *testing.T) {
	fakeJWT := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyXzEyMyIsImVtYWlsIjoiZGV2QGNvbXBhbnkuY29tIiwiZXhwIjoxODkzNDU2MDAwfQ.signature"
	dbPath := writeTestStateDB(t, fakeJWT, "dev@company.com", "pro")

	auth, err := ExtractLocalAuth(dbPath)
	if err != nil {
		t.Fatalf("ExtractLocalAuth: %v", err)
	}
	if auth.AccessToken != fakeJWT {
		t.Fatalf("AccessToken mismatch")
	}
	if auth.Email != "dev@company.com" {
		t.Fatalf("Email = %q", auth.Email)
	}
	if auth.MembershipType != "pro" {
		t.Fatalf("MembershipType = %q", auth.MembershipType)
	}
	if want := time.Unix(1893456000, 0).UTC(); !auth.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", auth.ExpiresAt, want)
	}
}

func TestExtractLocalAuth_MissingFile(t *testing.T) {
	if _, err := ExtractLocalAuth(filepath.Join(t.TempDir(), "missing.vscdb")); err == nil {
		t.Fatal("expected error for missing db")
	}
}

func TestStateDBCandidatesIncludesLinuxPath(t *testing.T) {
	cands := stateDBCandidates()
	if len(cands) == 0 {
		t.Fatal("expected at least one candidate path")
	}
}
