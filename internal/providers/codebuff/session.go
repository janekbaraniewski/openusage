package codebuff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type codebuffUsage struct {
	InputTokens             *int64   `json:"input_tokens,omitempty"`
	OutputTokens            *int64   `json:"output_tokens,omitempty"`
	CacheReadInputTokens    *int64   `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int64  `json:"cache_creation_input_tokens,omitempty"`
	Credits                 *float64 `json:"credits,omitempty"`
	Model                   string   `json:"model,omitempty"`
}

type codebuffMetadata struct {
	Usage     *codebuffUsage           `json:"usage,omitempty"`
	Codebuff  *codebuffMetadataInner   `json:"codebuff,omitempty"`
	RunState  *codebuffRunState        `json:"runState,omitempty"`
	Timestamp string                   `json:"timestamp,omitempty"`
}

type codebuffMetadataInner struct {
	Usage *codebuffUsage `json:"usage,omitempty"`
}

type codebuffRunState struct {
	SessionState *codebuffSessionState `json:"sessionState,omitempty"`
}

type codebuffSessionState struct {
	MainAgentState *codebuffMainAgentState `json:"mainAgentState,omitempty"`
}

type codebuffMainAgentState struct {
	MessageHistory []codebuffHistoryEntry `json:"messageHistory,omitempty"`
}

type codebuffHistoryEntry struct {
	ProviderOptions *codebuffProviderOptions `json:"providerOptions,omitempty"`
}

type codebuffProviderOptions struct {
	Usage *codebuffUsage `json:"usage,omitempty"`
}

type codebuffMessage struct {
	ID        string            `json:"id,omitempty"`
	Role      string            `json:"role,omitempty"`
	Timestamp string            `json:"timestamp,omitempty"`
	Metadata  *codebuffMetadata `json:"metadata,omitempty"`
}

type codebuffEntry struct {
	DedupKey  string
	Channel   string
	Project   string
	ChatID    string
	Model     string
	Provider  string
	Input     int64
	Output    int64
	CacheRead int64
	CacheWrite int64
	Credits   float64
	HasCredits bool
	Timestamp time.Time
}

func readAllChats(ctx context.Context, dataDirs []string) ([]codebuffEntry, error) {
	seen := make(map[string]struct{})
	var all []codebuffEntry

	for _, root := range dataDirs {
		if ctx.Err() != nil {
			return all, ctx.Err()
		}
		channel := filepath.Base(root)
		projectsDir := filepath.Join(root, "projects")
		if !dirExists(projectsDir) {
			continue
		}
		walkErr := filepath.WalkDir(projectsDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Base(path) != "chat-messages.json" {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			entries, perFileErr := readCodebuffChatFile(path)
			if perFileErr != nil {
				return nil
			}
			for _, e := range entries {
				e.Channel = channel
				rel, relErr := filepath.Rel(projectsDir, path)
				if relErr == nil {
					parts := strings.Split(filepath.ToSlash(rel), "/")
					if len(parts) > 0 {
						e.Project = parts[0]
					}
					if len(parts) >= 3 && parts[len(parts)-3] == "chats" {
						e.ChatID = parts[len(parts)-2]
					}
				}
				if e.Timestamp.IsZero() && e.ChatID != "" {
					if t, ok := parseChatIDTimestamp(e.ChatID); ok {
						e.Timestamp = t
					}
				}
				e.DedupKey = buildDedupKey(e)
				if _, dup := seen[e.DedupKey]; dup {
					continue
				}
				seen[e.DedupKey] = struct{}{}
				all = append(all, e)
			}
			return nil
		})
		if walkErr != nil {
			return all, walkErr
		}
	}
	return all, nil
}

func readCodebuffChatFile(path string) ([]codebuffEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("codebuff: reading %s: %w", path, err)
	}

	var messages []codebuffMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, nil
	}

	out := make([]codebuffEntry, 0, len(messages))
	ordinal := 0
	for _, msg := range messages {
		if !strings.EqualFold(msg.Role, "assistant") {
			continue
		}
		usage := pickUsage(msg.Metadata)
		if usage == nil {
			continue
		}
		in := derefInt(usage.InputTokens)
		out_ := derefInt(usage.OutputTokens)
		cr := derefInt(usage.CacheReadInputTokens)
		cw := derefInt(usage.CacheCreationInputTokens)
		if in == 0 && out_ == 0 && cr == 0 && cw == 0 {
			continue
		}

		model := strings.TrimSpace(usage.Model)
		if model == "" {
			model = "codebuff-unknown"
		}

		entry := codebuffEntry{
			Model:      model,
			Provider:   inferProvider(model),
			Input:      in,
			Output:     out_,
			CacheRead:  cr,
			CacheWrite: cw,
		}
		if usage.Credits != nil && *usage.Credits > 0 {
			entry.Credits = *usage.Credits
			entry.HasCredits = true
		}

		entry.Timestamp = pickTimestamp(msg)

		entry.DedupKey = stableMessageID(msg, ordinal, in, out_, cr, cw)
		ordinal++

		out = append(out, entry)
	}
	return out, nil
}

func pickUsage(meta *codebuffMetadata) *codebuffUsage {
	if meta == nil {
		return nil
	}
	if meta.Usage != nil {
		return meta.Usage
	}
	if meta.Codebuff != nil && meta.Codebuff.Usage != nil {
		return meta.Codebuff.Usage
	}
	if meta.RunState != nil && meta.RunState.SessionState != nil && meta.RunState.SessionState.MainAgentState != nil {
		for _, h := range meta.RunState.SessionState.MainAgentState.MessageHistory {
			if h.ProviderOptions != nil && h.ProviderOptions.Usage != nil {
				u := h.ProviderOptions.Usage
				if derefInt(u.InputTokens)+derefInt(u.OutputTokens)+derefInt(u.CacheReadInputTokens)+derefInt(u.CacheCreationInputTokens) > 0 {
					return u
				}
			}
		}
	}
	return nil
}

func pickTimestamp(msg codebuffMessage) time.Time {
	candidates := []string{}
	if msg.Metadata != nil && msg.Metadata.Timestamp != "" {
		candidates = append(candidates, msg.Metadata.Timestamp)
	}
	if msg.Timestamp != "" {
		candidates = append(candidates, msg.Timestamp)
	}
	for _, c := range candidates {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
			if t, err := time.Parse(layout, c); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}

func stableMessageID(msg codebuffMessage, ordinal int, in, out, cr, cw int64) string {
	if id := strings.TrimSpace(msg.ID); id != "" {
		return "id:" + id
	}
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%d|%d", in, out, cr, cw)
	return fmt.Sprintf("derived:%d:%s", ordinal, hex.EncodeToString(h.Sum(nil))[:16])
}

func buildDedupKey(e codebuffEntry) string {
	if strings.HasPrefix(e.DedupKey, "id:") {
		return e.DedupKey
	}
	return fmt.Sprintf("%s/%s/%s:%s", e.Channel, e.Project, e.ChatID, e.DedupKey)
}

func inferProvider(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(m, "claude-"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt-") || strings.HasPrefix(m, "o1-"):
		return "openai"
	case strings.HasPrefix(m, "gemini-"):
		return "google"
	default:
		return "unknown"
	}
}

func derefInt(v *int64) int64 {
	if v == nil {
		return 0
	}
	if *v < 0 {
		return 0
	}
	return *v
}
