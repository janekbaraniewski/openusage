package shared

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
)

// DefaultMaxLineBytes is the longest line IncrementalReader delivers. Longer
// lines are skipped (but still counted), matching the 10 MiB scanner buffers
// the providers used before.
const DefaultMaxLineBytes = 10 * 1024 * 1024

// guardBytes is how many bytes before the read offset are remembered to detect
// a file rewritten in place to the same or a larger size.
const guardBytes = 64

// IncrementalReader reads an append-only, newline-delimited file (JSONL or a
// plain log) a piece at a time. It remembers how far it got, so each Read only
// delivers lines appended since the previous one.
//
// Only complete lines (terminated by '\n') are consumed. A trailing partial
// line stays unconsumed and is returned as ReadResult.Tail, so a writer caught
// mid-line is re-read in full on the next call.
//
// A file that was replaced (different inode), truncated (smaller than the
// offset) or rewritten (the bytes just before the offset changed) is read again
// from the start and the result has Reset set; callers must then discard
// whatever they derived from earlier reads.
//
// The zero value is ready to use. An IncrementalReader is not safe for
// concurrent use.
type IncrementalReader struct {
	// MaxLineBytes overrides DefaultMaxLineBytes when positive.
	MaxLineBytes int

	info   os.FileInfo
	offset int64 // end of the last consumed line
	lines  int   // complete lines consumed
	guard  []byte
}

// ReadResult describes one IncrementalReader.Read call.
type ReadResult struct {
	// Changed is false when the file is byte-for-byte as it was at the
	// previous Read; no lines were delivered and Tail is nil.
	Changed bool
	// Reset is true when the reader started over from the beginning of the
	// file: on the first Read, and after a replace, truncate or rewrite.
	Reset bool
	// Tail holds the bytes after the last '\n' (a line still being written,
	// or a final line without a newline). It is not consumed.
	Tail []byte
	// TailLine is the line number Tail would have.
	TailLine int
}

// Offset returns the byte offset just past the last consumed line.
func (r *IncrementalReader) Offset() int64 { return r.offset }

// Lines returns the number of complete lines consumed so far.
func (r *IncrementalReader) Lines() int { return r.lines }

// Info returns the file info recorded at the last Read, or nil.
func (r *IncrementalReader) Info() os.FileInfo { return r.info }

// Forget drops all state so the next Read starts from the beginning.
func (r *IncrementalReader) Forget() {
	*r = IncrementalReader{MaxLineBytes: r.MaxLineBytes}
}

// Read delivers the complete lines appended to path since the previous call,
// with 1-based line numbers counted from the start of the file. Each line has
// its trailing "\n" (and "\r") removed; the slice is only valid during the
// callback.
//
// info may carry a fresh os.Stat/Lstat result the caller already has (for
// example from a directory walk); pass nil to have Read stat the file. On
// error the reader keeps its previous state.
func (r *IncrementalReader) Read(path string, info os.FileInfo, onLine func(line []byte, lineNo int)) (ReadResult, error) {
	if info == nil {
		var err error
		if info, err = os.Stat(path); err != nil {
			return ReadResult{}, err
		}
	}
	if r.info != nil && os.SameFile(r.info, info) &&
		info.Size() == r.info.Size() && info.ModTime().Equal(r.info.ModTime()) {
		return ReadResult{}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()

	size := info.Size()
	offset, lines := r.offset, r.lines
	reset := r.info == nil || !os.SameFile(r.info, info) || size < offset
	if !reset && len(r.guard) > 0 {
		buf := make([]byte, len(r.guard))
		if _, err := f.ReadAt(buf, offset-int64(len(buf))); err != nil || !bytes.Equal(buf, r.guard) {
			reset = true
		}
	}
	if reset {
		offset, lines = 0, 0
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ReadResult{}, err
	}
	maxLine := r.MaxLineBytes
	if maxLine <= 0 {
		maxLine = DefaultMaxLineBytes
	}
	consumed, n, tail, err := readLines(io.LimitReader(f, size-offset), maxLine, lines, onLine)
	if err != nil {
		return ReadResult{}, err
	}
	offset += consumed

	var guard []byte
	if offset > 0 {
		g := int64(guardBytes)
		if offset < g {
			g = offset
		}
		guard = make([]byte, g)
		if _, err := f.ReadAt(guard, offset-g); err != nil {
			guard = nil
		}
	}

	r.info = info
	r.offset = offset
	r.lines = n
	r.guard = guard
	return ReadResult{Changed: true, Reset: reset, Tail: tail, TailLine: n + 1}, nil
}

// readLines delivers each '\n'-terminated line of src to onLine, numbering
// from startLine+1. Lines longer than maxLine are counted but not delivered.
// It returns the bytes consumed (through the last '\n'), the last line number
// and the unterminated remainder, if any and if it fits in maxLine.
func readLines(src io.Reader, maxLine, startLine int, onLine func([]byte, int)) (int64, int, []byte, error) {
	br := bufio.NewReaderSize(src, 256*1024)
	var consumed, lineBytes int64
	lineNo := startLine
	var long []byte // the current line so far, when it spans buffer refills
	tooLong := false
	for {
		chunk, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			lineBytes += int64(len(chunk))
			if !tooLong {
				if len(long)+len(chunk) > maxLine {
					tooLong, long = true, nil
				} else {
					long = append(long, chunk...)
				}
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			if tooLong || len(long)+len(chunk) == 0 || len(long)+len(chunk) > maxLine {
				return consumed, lineNo, nil, nil
			}
			tail := make([]byte, 0, len(long)+len(chunk))
			tail = append(append(tail, long...), chunk...)
			return consumed, lineNo, tail, nil
		}
		if err != nil {
			return 0, 0, nil, err
		}
		consumed += lineBytes + int64(len(chunk))
		lineNo++
		if !tooLong && len(long)+len(chunk)-1 <= maxLine {
			line := chunk
			if len(long) > 0 {
				long = append(long, chunk...)
				line = long
			}
			line = line[:len(line)-1]
			if n := len(line); n > 0 && line[n-1] == '\r' {
				line = line[:n-1]
			}
			onLine(line, lineNo)
		}
		lineBytes, tooLong = 0, false
		if cap(long) > 1<<20 {
			long = nil
		} else {
			long = long[:0]
		}
	}
}
