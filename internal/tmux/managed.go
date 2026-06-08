package tmux

import "bytes"

// Managed config blocks.
//
// Several integrations write an openusage-owned block, bracketed by sentinel
// comments, into a user config file (tmux.conf, kitty.conf, ghostty config),
// and need to replace it in place on re-run or strip it on uninstall. These
// helpers are the one implementation of that edit, parameterized by the
// start/end markers, so every integration shares identical, tested semantics.
//
// The `block` passed to replaceOrAppendBlock already includes its own sentinel
// lines; the markers are used only to locate an existing block.

// blockPresent reports whether data contains the managed block (its start
// marker).
func blockPresent(data []byte, startMarker string) bool {
	return bytes.Contains(data, []byte(startMarker))
}

// replaceOrAppendBlock returns existing with the managed block replaced in
// place when present, or appended (separated by a blank line) when absent.
func replaceOrAppendBlock(existing []byte, startMarker, endMarker, block string) []byte {
	if !bytes.Contains(existing, []byte(startMarker)) {
		var out bytes.Buffer
		if len(existing) > 0 {
			out.Write(existing)
			if !bytes.HasSuffix(existing, []byte("\n")) {
				out.WriteByte('\n')
			}
			out.WriteByte('\n')
		}
		out.WriteString(block)
		return out.Bytes()
	}
	cleaned := removeBlock(existing, startMarker, endMarker)
	if len(cleaned) > 0 && !bytes.HasSuffix(cleaned, []byte("\n")) {
		cleaned = append(cleaned, '\n')
	}
	if len(cleaned) > 0 && !bytes.HasSuffix(cleaned, []byte("\n\n")) {
		cleaned = append(cleaned, '\n')
	}
	return append(cleaned, []byte(block)...)
}

// removeBlock returns existing with everything between (and including) the
// sentinel markers stripped, plus the trailing newline and one leading blank
// line so no orphan whitespace is left. Unbalanced markers (start without a
// matching end) leave the input unchanged.
func removeBlock(existing []byte, startMarker, endMarker string) []byte {
	startIdx := bytes.Index(existing, []byte(startMarker))
	if startIdx < 0 {
		return existing
	}
	endIdx := bytes.Index(existing[startIdx:], []byte(endMarker))
	if endIdx < 0 {
		return existing
	}
	endIdx += startIdx + len(endMarker)
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	leading := startIdx
	for leading > 0 && existing[leading-1] == '\n' {
		leading--
		if startIdx-leading >= 2 {
			break
		}
	}
	var out bytes.Buffer
	out.Write(existing[:leading])
	out.Write(existing[endIdx:])
	return out.Bytes()
}
