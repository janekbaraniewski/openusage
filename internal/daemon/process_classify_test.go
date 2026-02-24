package daemon

import (
	"fmt"
	"testing"
)

func TestClassifyEnsureError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  DaemonStatus
		wantMessage string
		wantHint    string
	}{
		{
			name:       "nil error returns running",
			err:        nil,
			wantStatus: DaemonStatusRunning,
		},
		{
			name:        "not installed returns friendly message",
			err:         fmt.Errorf("telemetry daemon service is not installed; run `openusage telemetry daemon install`"),
			wantStatus:  DaemonStatusNotInstalled,
			wantMessage: "Background helper is not set up.",
			wantHint:    "openusage telemetry daemon install",
		},
		{
			name:        "out of date returns error message",
			err:         fmt.Errorf("telemetry daemon is out of date (running=v0.3.0 expected=v0.4.0)"),
			wantStatus:  DaemonStatusOutdated,
			wantMessage: "telemetry daemon is out of date (running=v0.3.0 expected=v0.4.0)",
		},
		{
			name:        "unsupported on returns error status",
			err:         fmt.Errorf("auto-upgrade is unsupported on linux"),
			wantStatus:  DaemonStatusError,
			wantMessage: "auto-upgrade is unsupported on linux",
		},
		{
			name:        "generic error returns error status",
			err:         fmt.Errorf("connection refused"),
			wantStatus:  DaemonStatusError,
			wantMessage: "connection refused",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyEnsureError(tt.err)
			if got.Status != tt.wantStatus {
				t.Fatalf("ClassifyEnsureError(%v).Status = %v, want %v", tt.err, got.Status, tt.wantStatus)
			}
			if tt.wantMessage != "" && got.Message != tt.wantMessage {
				t.Fatalf("ClassifyEnsureError(%v).Message = %q, want %q", tt.err, got.Message, tt.wantMessage)
			}
			if tt.wantHint != "" && got.InstallHint != tt.wantHint {
				t.Fatalf("ClassifyEnsureError(%v).InstallHint = %q, want %q", tt.err, got.InstallHint, tt.wantHint)
			}
		})
	}
}
