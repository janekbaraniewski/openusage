package integrations

import (
	"encoding/json"
	"fmt"
)

// decodeJSONC unmarshals JSON that may carry JSONC extensions into v.
//
// OpenCode documents both `opencode.json` and `opencode.jsonc` as valid config
// formats (https://opencode.ai/docs/config/#format), and encoding/json rejects
// the comments and trailing commas a .jsonc file is allowed to contain. Strip
// those before handing the bytes to the standard decoder.
func decodeJSONC(data []byte, v any) error {
	if err := json.Unmarshal(stripJSONC(data), v); err != nil {
		return fmt.Errorf("integrations: decode jsonc: %w", err)
	}
	return nil
}

// stripJSONC removes // and /* */ comments and trailing commas from data,
// leaving byte offsets of the surviving content untouched where possible so
// decoder error positions stay meaningful. String literals are passed through
// verbatim so a "//" inside a value survives.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))

	const (
		text = iota
		inString
		inLineComment
		inBlockComment
	)

	state := text
	escaped := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		switch state {
		case inString:
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				state = text
			}

		case inLineComment:
			// Keep the newline so line numbers survive.
			if c == '\n' {
				out = append(out, c)
				state = text
			}

		case inBlockComment:
			// Keep newlines so line numbers survive.
			if c == '\n' {
				out = append(out, c)
				continue
			}
			if c == '*' && i+1 < len(data) && data[i+1] == '/' {
				i++
				state = text
			}

		default: // text
			switch {
			case c == '"':
				out = append(out, c)
				state = inString
			case c == '/' && i+1 < len(data) && data[i+1] == '/':
				i++
				state = inLineComment
			case c == '/' && i+1 < len(data) && data[i+1] == '*':
				i++
				state = inBlockComment
			default:
				out = append(out, c)
			}
		}
	}

	return removeTrailingCommas(out)
}

// removeTrailingCommas drops a comma that is followed only by whitespace and a
// closing brace or bracket. JSONC allows it; encoding/json does not.
func removeTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}

		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}

		if c == ',' {
			j := i + 1
			for j < len(data) && isJSONSpace(data[j]) {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				// Drop the comma, keep the whitespace that follows it.
				continue
			}
		}

		out = append(out, c)
	}

	return out
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
