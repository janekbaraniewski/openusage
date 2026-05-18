package pricing

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDiskCache_StoreAndLoad(t *testing.T) {
	c := NewDiskCacheAt(t.TempDir())
	c.SetTTL(time.Hour)

	payload := []byte(`{"hello":"world"}`)
	if err := c.Store("foo", payload); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, _, fresh, err := c.Load("foo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !fresh {
		t.Errorf("expected fresh")
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("payload roundtrip failed")
	}
}

func TestDiskCache_StaleAfterTTL(t *testing.T) {
	c := NewDiskCacheAt(t.TempDir())
	c.SetTTL(10 * time.Millisecond)

	if err := c.Store("k", []byte("v")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// rewrite mtime into the past
	path := c.Path("k")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	data, _, fresh, err := c.Load("k")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fresh {
		t.Errorf("expected stale after TTL window passed")
	}
	if len(data) == 0 {
		t.Errorf("stale entry should still return data for fallback use")
	}
}

func TestDiskCache_MissReturnsNoError(t *testing.T) {
	c := NewDiskCacheAt(t.TempDir())
	c.SetTTL(time.Hour)
	data, _, fresh, err := c.Load("nope")
	if err != nil {
		t.Errorf("miss should not error: %v", err)
	}
	if fresh || data != nil {
		t.Errorf("miss should yield no data")
	}
}

func TestDiskCache_AtomicWriteUnderConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCacheAt(dir)
	c.SetTTL(time.Hour)

	// generate two payloads that are clearly distinguishable
	a := bytes.Repeat([]byte("A"), 4096)
	b := bytes.Repeat([]byte("B"), 4096)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if err := c.Store("concurrent", a); err != nil {
					t.Errorf("Store a: %v", err)
					return
				}
				if err := c.Store("concurrent", b); err != nil {
					t.Errorf("Store b: %v", err)
					return
				}
			}
		}
	}()

	for i := 0; i < 200; i++ {
		data, _, _, err := c.Load("concurrent")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if data != nil && !bytes.Equal(data, a) && !bytes.Equal(data, b) {
			t.Fatalf("observed torn write: len=%d head=%q tail=%q", len(data), data[:8], data[len(data)-8:])
		}
	}
	close(stop)
	wg.Wait()

	// extra: ensure no stray .tmp files survived
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("found leftover tmp file: %s", e.Name())
		}
	}
}

func TestResolveTTL(t *testing.T) {
	t.Setenv("OPENUSAGE_PRICING_TTL", "")
	if got := ResolveTTL(); got != DefaultTTL {
		t.Errorf("unset = %v, want %v", got, DefaultTTL)
	}
	t.Setenv("OPENUSAGE_PRICING_TTL", "30m")
	if got := ResolveTTL(); got != 30*time.Minute {
		t.Errorf("30m = %v", got)
	}
	t.Setenv("OPENUSAGE_PRICING_TTL", "garbage")
	if got := ResolveTTL(); got != DefaultTTL {
		t.Errorf("garbage = %v, want default", got)
	}
	t.Setenv("OPENUSAGE_PRICING_TTL", "3600")
	if got := ResolveTTL(); got != time.Hour {
		t.Errorf("plain-seconds = %v, want 1h", got)
	}
}
