package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func forceCorruptIntegrityCheck(t *testing.T) {
	t.Helper()
	orig := integrityCheck
	integrityCheck = func(*sql.DB) (bool, string) { return true, "simulated corruption" }
	t.Cleanup(func() { integrityCheck = orig })
}

func ingestOneEvent(t *testing.T, store *Store) {
	t.Helper()
	tokens := int64(42)
	if _, err := store.Ingest(context.Background(), IngestRequest{
		SourceSystem:  SourceSystem("codex"),
		SourceChannel: SourceChannelJSONL,
		OccurredAt:    time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		ProviderID:    "codex",
		AgentName:     "codex",
		EventType:     EventTypeMessageUsage,
		SessionID:     "session-open-store",
		TurnID:        fmt.Sprintf("turn-%d", time.Now().UnixNano()),
		TokenUsage:    core.TokenUsage{TotalTokens: &tokens},
	}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
}

func corruptBackups(t *testing.T, dbPath string) []string {
	t.Helper()
	matches, err := filepath.Glob(dbPath + ".corrupt.*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return matches
}

// Regression test for #423: a second opener (e.g. the hook-ingest fallback in a
// short-lived process) must never quarantine, remove -shm from, or prune a
// database another Store has open, even if its integrity check fails.
func TestOpenStore_SecondOpenerDoesNotQuarantineLiveDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")

	first, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("first OpenStore: %v", err)
	}
	defer first.Close()
	ingestOneEvent(t, first)

	shmBefore, err := os.Stat(dbPath + "-shm")
	if err != nil {
		t.Fatalf("stat -shm: %v", err)
	}
	// An older corrupt backup the second opener must not prune.
	oldBackup := dbPath + ".corrupt.20200101T000000"
	if err := os.WriteFile(oldBackup, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old backup: %v", err)
	}

	forceCorruptIntegrityCheck(t)
	second, err := OpenStore(dbPath)
	if err == nil {
		second.Close()
		t.Fatalf("second OpenStore succeeded on a database reported corrupt; want an error instead of quarantine")
	}

	if got := corruptBackups(t, dbPath); len(got) != 1 || got[0] != oldBackup {
		t.Fatalf("corrupt backups = %v, want only %s (live DB must not be renamed, old backup must not be pruned)", got, oldBackup)
	}
	shmAfter, err := os.Stat(dbPath + "-shm")
	if err != nil {
		t.Fatalf("-shm removed by second opener: %v", err)
	}
	if !os.SameFile(shmBefore, shmAfter) {
		t.Fatalf("-shm was replaced by second opener")
	}

	// The first Store still writes to, and reads from, the file at dbPath.
	ingestOneEvent(t, first)
	check, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open check handle: %v", err)
	}
	defer check.Close()
	var n int
	if err := check.QueryRow(`SELECT COUNT(*) FROM usage_raw_events`).Scan(&n); err != nil {
		t.Fatalf("count raw events at dbPath: %v", err)
	}
	if n == 0 {
		t.Fatalf("database at %s has no events; live DB was swapped out", dbPath)
	}
}

// A second opener of a healthy database shares it normally.
func TestOpenStore_ConcurrentOpenersShareDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	first, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("first OpenStore: %v", err)
	}
	defer first.Close()

	second, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("second OpenStore: %v", err)
	}
	ingestOneEvent(t, second)
	if err := second.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Once every other Store is closed, the next opener is sole again.
	first.Close()
	lock, err := acquireDBOpenLock(dbPath)
	if err != nil {
		t.Fatalf("acquireDBOpenLock: %v", err)
	}
	defer lock.release()
	if !lock.soleOpener() {
		t.Fatalf("expected sole opener after all Stores closed")
	}
}

// The sole opener keeps the existing recovery behaviour: quarantine and start fresh.
func TestOpenStore_SoleOpenerQuarantinesCorruptDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	s, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	ingestOneEvent(t, s)
	s.Close()

	forceCorruptIntegrityCheck(t)
	s, err = OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore after corruption: %v", err)
	}
	defer s.Close()
	if got := corruptBackups(t, dbPath); len(got) != 1 {
		t.Fatalf("corrupt backups = %v, want exactly one quarantined DB", got)
	}
}
