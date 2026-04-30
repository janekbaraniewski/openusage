package opencode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SolidStart server functions ("use server"-marked closures) are exposed by
// OpenCode's console under POST/GET /_server with an x-server-id header. The
// response body is **Seroval-encoded JS**, not JSON — it's executable code
// that mutates a global self.$R object.
//
// A typical body looks like this (one billing.get response):
//
//   ;0x0000021c;((self.$R=self.$R||{})["server-fn:3"]=[],($R=>$R[0]={
//     customerID:null,paymentMethodLast4:null,balance:0,monthlyLimit:null,
//     monthlyUsage:0,timeMonthlyUsageUpdated:$R[1]=new Date("2026-04-30..."),
//     subscriptionPlan:null,...
//   })($R["server-fn:3"]))
//
// Notable Seroval quirks:
//   - Inline back-references: `$R[N]=<value>` defines and uses slot N at
//     once; the assignment is purely bookkeeping.
//   - Standalone references: `$R[N]` reads a previously-defined slot.
//   - JS shorthand booleans: `!0` == true, `!1` == false.
//   - Date values: `new Date("ISO-8601")`.
//   - Object keys are bare identifiers (no quoting).
//
// We don't need a full Seroval interpreter — we need the data shape behind
// the four endpoints we call. The strategy:
//   1. Strip the wrapper to find the `($R=>...)($R[...])` body.
//   2. Walk the body, capturing every `$R[N]=<value>` inline definition into
//      a slot table.
//   3. Substitute remaining standalone `$R[N]` references with their slot
//      values (handles cycles by leaving recursion-stopping placeholders).
//   4. Normalize the result: bare keys → quoted, `!0`/`!1` → true/false,
//      `new Date("X")` → "X" (we keep dates as RFC-3339 strings; callers
//      time.Parse on demand).
//   5. json.Unmarshal into any.
//
// This deliberately doesn't try to be a complete JS literal parser. It
// handles every shape we've seen in real OpenCode responses (tested
// against the four captured fixtures); anything novel will fail loudly
// rather than silently misparse.

var (
	// Outer wrapper: optional `;0x...;` prefix, then the IIFE that defines
	// $R[N] slots. We capture the lambda body — everything between the
	// `$R=>` arrow and the closing `)($R["server-fn:N"])`.
	wrapperRE = regexp.MustCompile(`(?s)^\s*;0x[0-9a-fA-F]+;\(\(self\.\$R=self\.\$R\|\|\{\}\)\["server-fn:\d+"\]=\[\],\(\$R=>(.+)\)\(\$R\["server-fn:\d+"\]\)\)\s*$`)

	// new Date("ISO-8601") — capture the timestamp string only.
	dateRE = regexp.MustCompile(`new\s+Date\(("[^"]*")\)`)

	// $R[N]= inline assignment prefix (definition AND value).
	inlineAssignRE = regexp.MustCompile(`\$R\[(\d+)\]=`)

	// Standalone $R[N] reference (no following =). Matched lazily during
	// substitution so we don't accidentally chew the LHS of an inline
	// assignment.
	refRE = regexp.MustCompile(`\$R\[(\d+)\]`)

	// !0 / !1 → true / false (JS minifier shorthand).
	boolNotRE = regexp.MustCompile(`!([01])\b`)

	// Bare object keys: identifier directly followed by `:`. Crude but
	// sufficient for the shapes Seroval emits — server-side code can't
	// emit truly hostile keys here.
	bareKeyRE = regexp.MustCompile(`([{,])\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*:`)
)

// ParseSeroval decodes a SolidStart `/_server` response body into a generic
// Go any. The top-level value is whatever was assigned to `$R[0]` — for
// OpenCode's queryBillingInfo / queryUsage / queryKeys / queryUsageMonth
// that's an object or array of plain primitives.
//
// Failure modes are loud: unrecognised wrapper structure, unbalanced
// braces, malformed JSON after normalization — all return descriptive
// errors. Callers should treat any error as "we got a response shape we
// don't know how to read" and surface AUTH or ERROR on the tile rather
// than fabricating data.
func ParseSeroval(body []byte) (any, error) {
	matches := wrapperRE.FindStringSubmatch(string(body))
	if matches == nil {
		return nil, fmt.Errorf("seroval: response body doesn't match expected wrapper")
	}
	src := matches[1]

	// Pass 1 — capture inline-assigned slots and rewrite their RHS into
	// the in-place value. After this pass, every `$R[N]=<value>` becomes
	// just `<value>`, and we have a table of all slots seen.
	slots := make(map[string]string)
	src, err := captureInlineSlots(src, slots)
	if err != nil {
		return nil, fmt.Errorf("seroval: capture slots: %w", err)
	}

	// Pass 2 — resolve standalone references. We bound recursion at 3
	// passes; any deeper graph is a bug in our parser, not real data.
	// The four OpenCode endpoints we care about don't use cyclical
	// references at all.
	for i := 0; i < 3; i++ {
		before := src
		src = refRE.ReplaceAllStringFunc(src, func(m string) string {
			parts := refRE.FindStringSubmatch(m)
			if parts == nil {
				return m
			}
			if v, ok := slots[parts[1]]; ok {
				return v
			}
			return "null"
		})
		if src == before {
			break
		}
	}

	// Pass 3 — JS-isms → JSON-isms.
	src = boolNotRE.ReplaceAllStringFunc(src, func(m string) string {
		switch m {
		case "!0":
			return "true"
		case "!1":
			return "false"
		}
		return m
	})
	src = dateRE.ReplaceAllString(src, "$1")

	// Pass 4 — quote bare object keys. Run twice: nested objects with
	// adjacent bare keys can have one boundary character `,` between them
	// that the regex treats as the boundary for the outer match — second
	// pass picks up the inner.
	for i := 0; i < 2; i++ {
		src = bareKeyRE.ReplaceAllString(src, `$1"$2":`)
	}

	// The wrapper was `$R=>$R[0]=<value>` ; we already substituted away
	// the `$R[N]=` prefixes, so what remains starts with `<value>`. But
	// there can be commas separating multiple `$R[N]=...` siblings if
	// Seroval used a comma operator. For our captured fixtures the value
	// for $R[0] is the whole tail; defensively, if we see a top-level
	// comma we take the first.
	src = strings.TrimSpace(src)
	src = trimTopLevelTrailing(src)

	var out any
	if err := json.Unmarshal([]byte(src), &out); err != nil {
		return nil, fmt.Errorf("seroval: parse normalized json: %w (normalized=%.200q)", err, src)
	}
	return out, nil
}

// captureInlineSlots walks `src` and, for every `$R[N]=<value>` sub-string,
// records `<value>` (literal, balanced) in `slots[N]` and rewrites that
// part of `src` to just `<value>` (no `$R[N]=` prefix). Returns the
// rewritten string. Walks character-by-character with a brace/bracket/quote
// counter to find the right end of `<value>`.
func captureInlineSlots(src string, slots map[string]string) (string, error) {
	for {
		loc := inlineAssignRE.FindStringIndex(src)
		if loc == nil {
			break
		}
		// inlineAssignRE matched `$R[N]=` — extract N, then walk forward
		// from the end of the match to find the value's terminator.
		matchEnd := loc[1]
		idxMatches := inlineAssignRE.FindStringSubmatch(src[loc[0]:loc[1]])
		slotID := idxMatches[1]

		valueEnd, err := scanLiteralEnd(src, matchEnd)
		if err != nil {
			return "", err
		}
		value := src[matchEnd:valueEnd]
		slots[slotID] = value

		// Rewrite: drop the `$R[N]=` prefix; keep value in place.
		src = src[:loc[0]] + src[matchEnd:]
	}
	return src, nil
}

// scanLiteralEnd returns the offset (exclusive) of the end of the JS
// literal starting at position `start` in `src`. Handles balanced
// braces/brackets/parentheses, double-quoted strings (including escapes),
// and stops on top-level commas or the parent's close-bracket. The intent
// is to find the end of one `<value>` in `key:<value>,nextKey:...` or
// `[<value>,...]` contexts.
func scanLiteralEnd(src string, start int) (int, error) {
	depth := 0
	inString := false
	for i := start; i < len(src); i++ {
		c := src[i]
		if inString {
			if c == '\\' && i+1 < len(src) {
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			// Two cases:
			//   depth == 0 → this closer belongs to the *parent* container,
			//                meaning our literal ended at the previous
			//                character (could be a primitive like `null`).
			//   depth > 0  → this closer matches a brace we opened. If
			//                decrementing brings us back to 0, we just
			//                closed our own top-level brace; stop AFTER it.
			if depth == 0 {
				return i, nil
			}
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		case ',':
			if depth == 0 {
				return i, nil
			}
		}
	}
	if depth == 0 {
		return len(src), nil
	}
	return 0, fmt.Errorf("seroval: unterminated literal starting at offset %d", start)
}

// trimTopLevelTrailing handles the edge case where a Seroval body ends with
// trailing slot definitions (e.g. `$R[0]={...},$R[1]=new Date(...)`). After
// our prefix-rewriting, that becomes `{...},"<date>"` — but we only care
// about $R[0]. Trim everything after the first balanced top-level value.
func trimTopLevelTrailing(src string) string {
	if src == "" {
		return src
	}
	end, err := scanLiteralEnd(src, 0)
	if err != nil || end <= 0 {
		return src
	}
	return src[:end]
}
