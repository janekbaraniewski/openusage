package shared

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	commitMatchLookback  = 2 * time.Minute
	commitMatchLookahead = 15 * time.Minute
	lateCommitPenalty    = 5 * time.Minute
)

var RunGitCommand = exec.CommandContext

type FileLineDelta struct {
	Added   int
	Removed int
}

type PatchStats struct {
	Added      int
	Removed    int
	Files      map[string]struct{}
	Deleted    map[string]struct{}
	FileDelta  map[string]FileLineDelta
	PatchCalls int
}

func NewPatchStats() PatchStats {
	return PatchStats{
		Files:     make(map[string]struct{}),
		Deleted:   make(map[string]struct{}),
		FileDelta: make(map[string]FileLineDelta),
	}
}

func ClonePatchStats(stats PatchStats) PatchStats {
	out := PatchStats{
		Added:      stats.Added,
		Removed:    stats.Removed,
		PatchCalls: stats.PatchCalls,
		Files:      make(map[string]struct{}, len(stats.Files)),
		Deleted:    make(map[string]struct{}, len(stats.Deleted)),
		FileDelta:  make(map[string]FileLineDelta, len(stats.FileDelta)),
	}
	for key := range stats.Files {
		out.Files[key] = struct{}{}
	}
	for key := range stats.Deleted {
		out.Deleted[key] = struct{}{}
	}
	for key, delta := range stats.FileDelta {
		out.FileDelta[key] = delta
	}
	return out
}

func ApplyTrackedChange(stats *PatchStats, paths []string, added, removed int, deleted bool) {
	if stats == nil {
		return
	}
	if stats.Files == nil {
		stats.Files = make(map[string]struct{})
	}
	if stats.Deleted == nil {
		stats.Deleted = make(map[string]struct{})
	}
	if stats.FileDelta == nil {
		stats.FileDelta = make(map[string]FileLineDelta)
	}
	stats.Added += added
	stats.Removed += removed

	var primary string
	for _, rawPath := range paths {
		path := NormalizeRepoRelativePath(rawPath)
		if path == "" {
			continue
		}
		stats.Files[path] = struct{}{}
		if deleted {
			stats.Deleted[path] = struct{}{}
		}
		if primary == "" {
			primary = path
		}
	}
	if primary == "" || (added == 0 && removed == 0) {
		return
	}
	delta := stats.FileDelta[primary]
	delta.Added += added
	delta.Removed += removed
	stats.FileDelta[primary] = delta
}

func BestEffortWorkingDir(cwd string, paths []string, stats PatchStats) string {
	if path := strings.TrimSpace(cwd); path != "" {
		return path
	}
	for _, path := range paths {
		if dir := candidateWorkingDir(path); dir != "" {
			return dir
		}
	}
	fileDeltaPaths := make([]string, 0, len(stats.FileDelta))
	for path := range stats.FileDelta {
		fileDeltaPaths = append(fileDeltaPaths, path)
	}
	sort.Strings(fileDeltaPaths)
	for _, path := range fileDeltaPaths {
		if dir := candidateWorkingDir(path); dir != "" {
			return dir
		}
	}
	allPaths := make([]string, 0, len(stats.Files))
	for path := range stats.Files {
		allPaths = append(allPaths, path)
	}
	sort.Strings(allPaths)
	for _, path := range allPaths {
		if dir := candidateWorkingDir(path); dir != "" {
			return dir
		}
	}
	return ""
}

type GitCommitCandidate struct {
	CWD        string
	OccurredAt time.Time
	Message    string
	Patch      PatchStats
}

type GitCommitStat struct {
	Hash       string
	OccurredAt time.Time
	Subject    string
	Added      int
	Removed    int
	FileDelta  map[string]FileLineDelta
}

type GitCommitAttribution struct {
	MatchedCommits   int
	UnmatchedCommits int
	LinesAdded       int
	LinesRemoved     int
	AIAdded          int
	AIRemoved        int
	Files            map[string]struct{}
}

func CollectGitCommitAttribution(ctx context.Context, candidates []GitCommitCandidate) GitCommitAttribution {
	if len(candidates) == 0 {
		return GitCommitAttribution{}
	}

	rootCache := make(map[string]string)
	grouped := make(map[string][]GitCommitCandidate)
	attr := GitCommitAttribution{
		UnmatchedCommits: len(candidates),
		Files:            make(map[string]struct{}),
	}

	for _, candidate := range candidates {
		root := resolveGitRepoRoot(ctx, candidate.CWD, rootCache)
		if root == "" {
			continue
		}
		grouped[root] = append(grouped[root], candidate)
	}

	for repoRoot, repoCandidates := range grouped {
		stats := loadGitCommitStats(ctx, repoRoot, repoCandidates)
		if len(stats) == 0 {
			continue
		}
		sort.Slice(repoCandidates, func(i, j int) bool {
			return repoCandidates[i].OccurredAt.Before(repoCandidates[j].OccurredAt)
		})
		used := make([]bool, len(stats))
		for _, candidate := range repoCandidates {
			idx := matchGitCommit(candidate, stats, used)
			if idx < 0 {
				continue
			}
			used[idx] = true
			stat := stats[idx]
			attr.MatchedCommits++
			attr.UnmatchedCommits--
			attr.LinesAdded += stat.Added
			attr.LinesRemoved += stat.Removed
			for path := range stat.FileDelta {
				attr.Files[path] = struct{}{}
			}
			aiAdd, aiRemove := overlapPatchWithCommit(candidate.Patch, stat)
			attr.AIAdded += aiAdd
			attr.AIRemoved += aiRemove
		}
	}

	if attr.UnmatchedCommits < 0 {
		attr.UnmatchedCommits = 0
	}
	return attr
}

func resolveGitRepoRoot(ctx context.Context, cwd string, cache map[string]string) string {
	path := strings.TrimSpace(cwd)
	if path == "" {
		return ""
	}
	if root, ok := cache[path]; ok {
		return root
	}
	cmd := RunGitCommand(ctx, "git", "-C", path, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		cache[path] = ""
		return ""
	}
	root := strings.TrimSpace(string(output))
	cache[path] = root
	return root
}

func loadGitCommitStats(ctx context.Context, repoRoot string, candidates []GitCommitCandidate) []GitCommitStat {
	if strings.TrimSpace(repoRoot) == "" || len(candidates) == 0 {
		return nil
	}

	minTime := candidates[0].OccurredAt
	maxTime := candidates[0].OccurredAt
	for _, candidate := range candidates[1:] {
		if candidate.OccurredAt.Before(minTime) {
			minTime = candidate.OccurredAt
		}
		if candidate.OccurredAt.After(maxTime) {
			maxTime = candidate.OccurredAt
		}
	}

	cmd := RunGitCommand(
		ctx,
		"git", "-C", repoRoot,
		"log",
		"--no-merges",
		"--reverse",
		fmt.Sprintf("--since=@%d", minTime.Add(-commitMatchLookback).Unix()),
		fmt.Sprintf("--until=@%d", maxTime.Add(commitMatchLookahead).Unix()),
		"--numstat",
		"--format=format:commit%x1f%H%x1f%ct%x1f%s",
	)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseGitCommitStats(string(output))
}

func parseGitCommitStats(raw string) []GitCommitStat {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var out []GitCommitStat
	var current *GitCommitStat

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "commit\x1f") {
			parts := strings.Split(line, "\x1f")
			if len(parts) < 4 {
				current = nil
				continue
			}
			ts, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
			if err != nil {
				current = nil
				continue
			}
			out = append(out, GitCommitStat{
				Hash:       strings.TrimSpace(parts[1]),
				OccurredAt: time.Unix(ts, 0).UTC(),
				Subject:    normalizeCommitMessage(parts[3]),
				FileDelta:  make(map[string]FileLineDelta),
			})
			current = &out[len(out)-1]
			continue
		}
		if current == nil {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		added, errAdd := strconv.Atoi(strings.TrimSpace(parts[0]))
		removed, errRemove := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errAdd != nil || errRemove != nil {
			continue
		}
		path := NormalizeRepoRelativePath(parts[2])
		if path == "" {
			continue
		}
		current.Added += added
		current.Removed += removed
		current.FileDelta[path] = FileLineDelta{Added: added, Removed: removed}
	}

	return out
}

func matchGitCommit(candidate GitCommitCandidate, commits []GitCommitStat, used []bool) int {
	bestIdx := -1
	bestScore := time.Duration(1<<63 - 1)
	normalizedMessage := normalizeCommitMessage(candidate.Message)

	for idx, stat := range commits {
		if used[idx] {
			continue
		}
		delta := stat.OccurredAt.Sub(candidate.OccurredAt)
		if delta < -commitMatchLookback || delta > commitMatchLookahead {
			continue
		}
		score := delta
		if score < 0 {
			score = -score + lateCommitPenalty
		}
		if normalizedMessage != "" && stat.Subject != "" && normalizedMessage == stat.Subject {
			score -= time.Minute
		}
		if score < bestScore {
			bestScore = score
			bestIdx = idx
		}
	}

	return bestIdx
}

func overlapPatchWithCommit(patch PatchStats, commit GitCommitStat) (int, int) {
	if len(patch.FileDelta) == 0 || len(commit.FileDelta) == 0 {
		return 0, 0
	}
	added := 0
	removed := 0
	for path, patchDelta := range patch.FileDelta {
		commitDelta, ok := commit.FileDelta[NormalizeRepoRelativePath(path)]
		if !ok {
			for commitPath, candidate := range commit.FileDelta {
				if strings.HasSuffix(NormalizeRepoRelativePath(path), "/"+commitPath) || NormalizeRepoRelativePath(path) == commitPath {
					commitDelta = candidate
					ok = true
					break
				}
			}
		}
		if !ok {
			continue
		}
		added += min(patchDelta.Added, commitDelta.Added)
		removed += min(patchDelta.Removed, commitDelta.Removed)
	}
	return added, removed
}

func NormalizeRepoRelativePath(path string) string {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return ""
	}
	return strings.ReplaceAll(normalized, "\\", "/")
}

func ExtractGitCommitMessage(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return ""
	}
	tokens := shellLikeSplit(trimmed)
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch {
		case token == "-m" || token == "--message":
			if i+1 < len(tokens) {
				return normalizeCommitMessage(tokens[i+1])
			}
		case strings.HasPrefix(token, "-m="):
			return normalizeCommitMessage(strings.TrimPrefix(token, "-m="))
		case strings.HasPrefix(token, "--message="):
			return normalizeCommitMessage(strings.TrimPrefix(token, "--message="))
		}
	}
	return ""
}

func normalizeCommitMessage(msg string) string {
	return strings.TrimSpace(strings.Trim(msg, `"'`))
}

func shellLikeSplit(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	var (
		out        []string
		current    strings.Builder
		inSingle   bool
		inDouble   bool
		escapeNext bool
	)
	flush := func() {
		if current.Len() == 0 {
			return
		}
		out = append(out, current.String())
		current.Reset()
	}
	for _, r := range input {
		switch {
		case escapeNext:
			current.WriteRune(r)
			escapeNext = false
		case r == '\\' && !inSingle:
			escapeNext = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case (r == ' ' || r == '\t' || r == '\n') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return out
}

func candidateWorkingDir(path string) string {
	cleaned := strings.TrimSpace(path)
	if !filepath.IsAbs(cleaned) {
		return ""
	}
	cleaned = filepath.Clean(cleaned)
	base := filepath.Base(cleaned)
	if filepath.Ext(base) != "" || strings.Contains(base, ".") {
		return filepath.Dir(cleaned)
	}
	return cleaned
}
