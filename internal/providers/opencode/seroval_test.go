package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

// loadFixture reads a captured Seroval response from testdata/.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// queryBillingInfo (action ID c83b78a614689c38...) — the most important
// surface for our tile. Verify each field we care about lands on a Go map.
func TestParseSeroval_BillingInfo(t *testing.T) {
	body := loadFixture(t, "seroval_c83b78a61468.txt")
	parsed, err := ParseSeroval(body)
	if err != nil {
		t.Fatalf("ParseSeroval error: %v", err)
	}
	m, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T: %#v", parsed, parsed)
	}

	// Fields present and zero-valued for a fresh, non-billed account
	// (matches the captured response from the user's account):
	cases := []struct {
		key  string
		want any
	}{
		{"customerID", nil},
		{"paymentMethodID", nil},
		{"paymentMethodLast4", nil},
		{"balance", float64(0)},
		{"reload", nil},
		{"reloadAmount", float64(20)},
		{"reloadAmountMin", float64(10)},
		{"reloadTrigger", float64(5)},
		{"reloadTriggerMin", float64(5)},
		{"monthlyLimit", nil},
		{"monthlyUsage", float64(0)},
		{"subscription", nil},
		{"subscriptionID", nil},
		{"subscriptionPlan", nil},
		{"lite", nil},
		{"liteSubscriptionID", nil},
	}
	for _, tc := range cases {
		got, ok := m[tc.key]
		if !ok {
			t.Errorf("missing key %q in parsed billing info", tc.key)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %v (%T), want %v", tc.key, got, got, tc.want)
		}
	}

	// The Date field gets normalized to its inner ISO-8601 string.
	if got := m["timeMonthlyUsageUpdated"]; got != "2026-04-30T11:32:46.000Z" {
		t.Errorf("timeMonthlyUsageUpdated = %v, want '2026-04-30T11:32:46.000Z'", got)
	}
}

// queryKeys (action ID c22cd964237b...) — array of key objects with
// inline slot definitions for each entry.
func TestParseSeroval_Keys(t *testing.T) {
	body := loadFixture(t, "seroval_c22cd964237b.txt")
	parsed, err := ParseSeroval(body)
	if err != nil {
		t.Fatalf("ParseSeroval error: %v", err)
	}
	arr, ok := parsed.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", parsed)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	if first["name"] != "tete" {
		t.Errorf("first key name = %v, want 'tete'", first["name"])
	}
	if first["timeUsed"] != nil {
		t.Errorf("first key timeUsed = %v, want nil", first["timeUsed"])
	}

	second := arr[1].(map[string]any)
	if second["name"] != "Default API Key" {
		t.Errorf("second key name = %v", second["name"])
	}
	// Date got normalized to a string here.
	if got := second["timeUsed"]; got != "2026-04-30T11:32:46.000Z" {
		t.Errorf("second key timeUsed = %v", got)
	}
	if second["keyDisplay"] != "sk-iUqX...be0i" {
		t.Errorf("second key keyDisplay = %v", second["keyDisplay"])
	}
}

// queryUsageMonth (action ID 15702f3a12ff...) — POST body returning
// nested {usage: [...], keys: [...]} structure with `!1` shorthand for
// false on the deleted flag.
func TestParseSeroval_UsageMonth(t *testing.T) {
	body := loadFixture(t, "seroval_15702f3a12ff.txt")
	parsed, err := ParseSeroval(body)
	if err != nil {
		t.Fatalf("ParseSeroval error: %v", err)
	}
	m, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", parsed)
	}

	usage, ok := m["usage"].([]any)
	if !ok {
		t.Fatalf("usage not an array: %T", m["usage"])
	}
	if len(usage) != 2 {
		t.Fatalf("usage entries = %d, want 2", len(usage))
	}
	first := usage[0].(map[string]any)
	if first["model"] != "gpt-5-nano" {
		t.Errorf("first model = %v", first["model"])
	}
	if first["totalCost"] != float64(0) {
		t.Errorf("first totalCost = %v", first["totalCost"])
	}

	keys, ok := m["keys"].([]any)
	if !ok {
		t.Fatalf("keys not an array")
	}
	if len(keys) != 2 {
		t.Fatalf("keys count = %d, want 2", len(keys))
	}
	// `!1` should have decoded to `false`.
	for i, k := range keys {
		km := k.(map[string]any)
		if km["deleted"] != false {
			t.Errorf("keys[%d].deleted = %v, want false", i, km["deleted"])
		}
	}
}

// Malformed wrapper → loud error rather than silent misparse.
func TestParseSeroval_RejectsUnrecognisedWrapper(t *testing.T) {
	cases := [][]byte{
		[]byte(""),
		[]byte("not seroval at all"),
		[]byte(`;0xabc;{"hello":"world"}`),                           // inner JSON, missing IIFE
		[]byte(`;0xabc;((self.$R={})["server-fn:0"]=[],$R[0]=null)`), // missing the arrow lambda
	}
	for i, body := range cases {
		_, err := ParseSeroval(body)
		if err == nil {
			t.Errorf("case %d: expected error for malformed body", i)
		}
	}
}

// Boolean shorthand: `!0` → true, `!1` → false. Synthetic minimal fixture.
func TestParseSeroval_BooleanShorthand(t *testing.T) {
	body := []byte(`;0x0010;((self.$R=self.$R||{})["server-fn:0"]=[],($R=>$R[0]={a:!0,b:!1,c:0})($R["server-fn:0"]))`)
	parsed, err := ParseSeroval(body)
	if err != nil {
		t.Fatal(err)
	}
	m := parsed.(map[string]any)
	if m["a"] != true {
		t.Errorf("a = %v, want true", m["a"])
	}
	if m["b"] != false {
		t.Errorf("b = %v, want false", m["b"])
	}
	if m["c"] != float64(0) {
		t.Errorf("c = %v, want 0", m["c"])
	}
}

// String escapes inside cookie-style strings shouldn't fool the bare-key
// quoter. Synthetic fixture using a key:value with a comma in the string.
func TestParseSeroval_StringWithCommas(t *testing.T) {
	body := []byte(`;0x0020;((self.$R=self.$R||{})["server-fn:0"]=[],($R=>$R[0]={s:"a,b,c",n:5})($R["server-fn:0"]))`)
	parsed, err := ParseSeroval(body)
	if err != nil {
		t.Fatal(err)
	}
	m := parsed.(map[string]any)
	if m["s"] != "a,b,c" {
		t.Errorf("s = %q, want 'a,b,c'", m["s"])
	}
	if m["n"] != float64(5) {
		t.Errorf("n = %v, want 5", m["n"])
	}
}
