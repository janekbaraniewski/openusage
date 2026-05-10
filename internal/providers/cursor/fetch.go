package cursor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   p.now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}
	if acct.RuntimeHints != nil {
		if email := strings.TrimSpace(acct.RuntimeHints["email"]); email != "" {
			snap.Raw["account_email"] = email
		}
		if membership := strings.TrimSpace(acct.RuntimeHints["membership"]); membership != "" {
			snap.Raw["membership_type"] = membership
		}
	}

	normalizeLegacyPaths(&acct)
	trackingDBPath := acct.Path("tracking_db", "")
	stateDBPath := acct.Path("state_db", "")

	token := acct.Token
	if token == "" && stateDBPath != "" {
		token = extractTokenFromStateDB(stateDBPath)
	}

	type apiResult struct {
		snap *core.UsageSnapshot
		err  error
	}
	apiCh := make(chan apiResult, 1)
	if token != "" {
		go func() {
			apiCtx, apiCancel := context.WithTimeout(ctx, 10*time.Second)
			defer apiCancel()
			apiSnap := core.UsageSnapshot{
				AccountID:   acct.ID,
				Metrics:     make(map[string]core.Metric),
				Resets:      make(map[string]time.Time),
				Raw:         make(map[string]string),
				DailySeries: make(map[string][]core.TimePoint),
			}
			err := p.fetchFromAPI(apiCtx, token, &apiSnap)
			apiCh <- apiResult{snap: &apiSnap, err: err}
		}()
	} else {
		apiCh <- apiResult{err: fmt.Errorf("no token")}
	}

	if acct.RuntimeHints == nil {
		acct.RuntimeHints = make(map[string]string)
	}
	if acct.RuntimeHints["tracking_db"] == "" && trackingDBPath != "" {
		acct.RuntimeHints["tracking_db"] = trackingDBPath
		acct.SetHint("tracking_db", trackingDBPath)
	}
	if acct.RuntimeHints["state_db"] == "" && stateDBPath != "" {
		acct.RuntimeHints["state_db"] = stateDBPath
		acct.SetHint("state_db", stateDBPath)
	}

	var hasLocalData bool
	if trackingDBPath != "" {
		before := cursorSnapshotDataSignature(&snap)
		if err := p.readTrackingDB(ctx, trackingDBPath, &snap); err != nil {
			log.Printf("[cursor] tracking DB error: %v", err)
			snap.Raw["tracking_db_error"] = err.Error()
		} else if cursorSnapshotDataSignature(&snap) != before {
			hasLocalData = true
		}
	}
	if stateDBPath != "" {
		before := cursorSnapshotDataSignature(&snap)
		if err := p.readStateDB(ctx, stateDBPath, &snap); err != nil {
			log.Printf("[cursor] state DB error: %v", err)
			snap.Raw["state_db_error"] = err.Error()
		} else if cursorSnapshotDataSignature(&snap) != before {
			hasLocalData = true
		}
	}

	ar := <-apiCh
	hasAPIData := false
	if ar.err == nil && ar.snap != nil {
		mergeAPIIntoSnapshot(&snap, ar.snap)
		hasAPIData = true
	} else if ar.err != nil && token != "" {
		log.Printf("[cursor] API fetch failed, falling back to local data: %v", ar.err)
		snap.Raw["api_error"] = ar.err.Error()
	}

	if !hasAPIData && !hasLocalData {
		snap.Status = core.StatusError
		snap.Message = "No Cursor tracking data accessible (no API token and no local DBs)"
		return snap, nil
	}

	if !hasAPIData {
		p.applyCachedModelAggregations(acct.ID, "", "", &snap)
		p.applyCachedBillingMetrics(acct.ID, &snap)
		p.buildLocalOnlyMessage(&snap)
	}

	p.ensureCreditGauges(acct.ID, &snap)

	return snap, nil
}
