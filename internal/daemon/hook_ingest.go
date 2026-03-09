package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

type HookParseResult struct {
	SourceName         string
	EffectiveAccountID string
	Requests           []telemetry.IngestRequest
	Warnings           []string
}

func ParseHookRequests(sourceName, accountID string, payload []byte) (HookParseResult, error) {
	sourceName = strings.TrimSpace(sourceName)
	source, ok := providers.TelemetrySourceBySystem(sourceName)
	if !ok {
		return HookParseResult{}, fmt.Errorf("unknown hook source %q", sourceName)
	}

	options, effectiveAccountID, warnings := ResolveTelemetrySourceOptions(source, strings.TrimSpace(accountID))
	reqs, err := telemetry.ParseSourceHookPayload(source, payload, options, effectiveAccountID)
	if err != nil {
		return HookParseResult{}, fmt.Errorf("parse hook payload: %w", err)
	}

	return HookParseResult{
		SourceName:         sourceName,
		EffectiveAccountID: effectiveAccountID,
		Requests:           reqs,
		Warnings:           warnings,
	}, nil
}

func IngestHookLocally(
	ctx context.Context,
	sourceName string,
	accountID string,
	payload []byte,
	dbPath string,
	spoolDir string,
	spoolOnly bool,
) (HookResponse, error) {
	parsed, err := ParseHookRequests(sourceName, accountID, payload)
	if err != nil {
		return HookResponse{}, err
	}
	return ingestParsedHookLocally(ctx, parsed, dbPath, spoolDir, spoolOnly)
}
