package claude_code

import (
	"os"
	"sort"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// conversationFile is the incremental parse state of one conversation JSONL
// file. Its records are exactly what parseConversationRecords would return
// for the bytes read so far (streamed lines merged, then sorted by timestamp
// with line order breaking ties), but each poll only decodes appended lines.
type conversationFile struct {
	entry      *jsonlCacheEntry
	reader     shared.IncrementalReader
	agentLabel string
	// streamIdx maps a streaming merge key to the index in entry.records of
	// the record later lines with the same key merge into.
	streamIdx map[string]int
	// provisional is set when the records include a final line without a
	// trailing newline. Such a line may still grow, so the next change to
	// the file re-reads it from the start.
	provisional bool

	change fileChange
}

type fileChangeKind int

const (
	fileUnchanged fileChangeKind = iota
	// fileAppended: records [oldLen:] are new and sort after every older
	// record of the file; records in modified (indices < oldLen) absorbed
	// streamed lines without moving.
	fileAppended
	// fileRebuilt: the records were re-read or re-sorted; nothing from the
	// previous merge can be reused for this file.
	fileRebuilt
)

type fileChange struct {
	kind   fileChangeKind
	oldLen int
	// modified holds, per changed record index (< oldLen), a copy of the
	// record as it was before this poll merged streamed lines into it.
	modified map[int]conversationRecord
}

// recordLess orders records the way a stable sort by timestamp of the
// line-ordered records does.
func recordLess(a, b *conversationRecord) bool {
	if a.timestamp.Equal(b.timestamp) {
		return a.lineNumber < b.lineNumber
	}
	return a.timestamp.Before(b.timestamp)
}

// update reads whatever was appended to the file since the last call and
// records how the entry changed in f.change.
func (f *conversationFile) update(path string, info os.FileInfo) {
	f.change = fileChange{}
	if f.entry == nil {
		f.entry = &jsonlCacheEntry{}
	}
	if f.provisional && info != nil {
		if prev := f.reader.Info(); prev == nil || prev.Size() != info.Size() || !prev.ModTime().Equal(info.ModTime()) {
			f.reader.Forget()
		}
	}

	var raw []conversationRecord
	res, err := f.reader.Read(path, info, func(line []byte, lineNo int) {
		if rec, ok := conversationRecordFromLine(line, lineNo, path, ""); ok {
			raw = append(raw, rec)
		}
	})
	if err != nil {
		// Unreadable (e.g. deleted between the walk and the read): no records,
		// as a failed full parse would give.
		f.reader.Forget()
		f.reset()
		f.change.kind = fileRebuilt
		return
	}
	if !res.Changed {
		return
	}
	isNew := f.entry.records == nil && f.streamIdx == nil
	if res.Reset {
		f.reset()
		f.agentLabel = detectAgentType(path)
	}
	f.provisional = false
	if len(res.Tail) > 0 {
		if rec, ok := conversationRecordFromLine(res.Tail, res.TailLine, path, ""); ok {
			raw = append(raw, rec)
			f.provisional = true
		}
	}
	for i := range raw {
		raw[i].agentID = f.agentLabel
	}

	if res.Reset {
		f.entry.records = mergeStreamingDuplicates(raw)
		f.resort()
		if isNew {
			// A file seen for the first time is all new records.
			f.change.kind = fileAppended
		} else {
			f.change.kind = fileRebuilt
		}
		return
	}
	f.appendRecords(raw)
}

// reset drops every record derived from earlier reads.
func (f *conversationFile) reset() {
	e := f.entry
	*e = jsonlCacheEntry{}
	f.streamIdx = nil
	f.provisional = false
}

// resort sorts all records and recomputes every derived index.
func (f *conversationFile) resort() {
	e := f.entry
	sort.SliceStable(e.records, func(i, j int) bool { return recordLess(&e.records[i], &e.records[j]) })
	e.usageKeys = make([]string, len(e.records))
	e.toolKeys = make([][]string, len(e.records))
	f.streamIdx = make(map[string]int, len(e.records))
	for i := range e.records {
		f.indexRecord(i)
	}
	e.allTime = nil
	e.costs = nil
	e.lostUsage = nil
	e.lostTools = nil
}

// indexRecord (re)computes the dedup keys and streaming index for record i.
func (f *conversationFile) indexRecord(i int) {
	e := f.entry
	rec := e.records[i]
	if key := streamingMergeKey(rec); key != "" && rec.usage != nil {
		if _, ok := f.streamIdx[key]; !ok {
			f.streamIdx[key] = i
		}
	}
	e.usageKeys[i] = ""
	if rec.usage != nil {
		e.usageKeys[i] = conversationUsageDedupKey(rec)
	}
	e.toolKeys[i] = nil
	for idx, item := range rec.content {
		if item.Type != "tool_use" {
			continue
		}
		if e.toolKeys[i] == nil {
			e.toolKeys[i] = make([]string, len(rec.content))
		}
		e.toolKeys[i][idx] = conversationToolDedupKey(rec, idx, item)
	}
}

// appendRecords folds newly read records (in line order) into the entry the
// way mergeStreamingDuplicates would have over the whole file.
func (f *conversationFile) appendRecords(raw []conversationRecord) {
	e := f.entry
	oldLen := len(e.records)
	if len(raw) == 0 {
		f.change = fileChange{kind: fileAppended, oldLen: oldLen}
		return
	}
	modified := make(map[int]conversationRecord)
	var fresh []conversationRecord
	freshIdx := make(map[string]int)
	reorder := false

	for _, rec := range raw {
		key := streamingMergeKey(rec)
		if key == "" || rec.usage == nil {
			fresh = append(fresh, rec)
			continue
		}
		if i, ok := f.streamIdx[key]; ok {
			if _, saved := modified[i]; !saved {
				modified[i] = cloneRecord(e.records[i])
			}
			before := e.records[i].timestamp
			mergeStreamedRecord(&e.records[i], rec)
			if !e.records[i].timestamp.Equal(before) {
				reorder = true
			}
			continue
		}
		if i, ok := freshIdx[key]; ok {
			mergeStreamedRecord(&fresh[i], rec)
			continue
		}
		freshIdx[key] = len(fresh)
		fresh = append(fresh, rec)
	}

	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].timestamp.Before(fresh[j].timestamp) })
	if len(fresh) > 0 && oldLen > 0 && recordLess(&fresh[0], &e.records[oldLen-1]) {
		reorder = true
	}
	e.records = append(e.records, fresh...)
	if reorder {
		f.resort()
		f.change = fileChange{kind: fileRebuilt}
		return
	}

	e.usageKeys = append(e.usageKeys, make([]string, len(fresh))...)
	e.toolKeys = append(e.toolKeys, make([][]string, len(fresh))...)
	for i := range modified {
		f.indexRecord(i)
	}
	for i := oldLen; i < len(e.records); i++ {
		f.indexRecord(i)
	}
	f.change = fileChange{kind: fileAppended, oldLen: oldLen, modified: modified}
}

// mergeStreamedRecord merges a later streamed line into dst exactly as
// mergeStreamingDuplicates does.
func mergeStreamedRecord(dst *conversationRecord, rec conversationRecord) {
	mergeUsageMax(dst.usage, rec.usage)
	dst.content = appendNewContentBlocks(dst.content, rec.content)
	if rec.timestamp.Before(dst.timestamp) {
		dst.timestamp = rec.timestamp
	}
	if rec.costUSD != nil {
		if dst.costUSD == nil || *rec.costUSD > *dst.costUSD {
			dst.costUSD = rec.costUSD
		}
	}
}

// cloneRecord copies the parts of r that merging mutates in place.
func cloneRecord(r conversationRecord) conversationRecord {
	if r.usage != nil {
		u := *r.usage
		if u.CacheCreation != nil {
			c := *u.CacheCreation
			u.CacheCreation = &c
		}
		if u.ServerToolUse != nil {
			s := *u.ServerToolUse
			u.ServerToolUse = &s
		}
		r.usage = &u
	}
	r.content = r.content[:len(r.content):len(r.content)]
	return r
}
