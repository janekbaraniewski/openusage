package daemon

import (
	"strings"
	"testing"
)

func TestIsTransientExecutablePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "empty path",
			path: "",
			want: true,
		},
		{
			name: "go run temp path",
			path: "/var/folders/ab/cd/T/go-build123456789/b001/exe/openusage",
			want: true,
		},
		{
			name: "stable binary path",
			path: "/usr/local/bin/openusage",
			want: false,
		},
		{
			name: "repo binary path",
			path: "/Users/example/work/openusage/bin/openusage",
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientExecutablePath(tt.path)
			if got != tt.want {
				t.Fatalf("isTransientExecutablePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestInstallRejectsTransientExecutablePath(t *testing.T) {
	manager := ServiceManager{
		Kind:    "darwin",
		exePath: "/var/folders/ab/cd/T/go-build123456789/b001/exe/openusage",
	}
	err := manager.Install()
	if err == nil {
		t.Fatal("Install() error = nil, want transient executable rejection")
	}
	if !strings.Contains(err.Error(), "transient executable") {
		t.Fatalf("Install() error = %q, want transient executable hint", err)
	}
}

func TestParseLSOFFirstRecord(t *testing.T) {
	out := strings.Join([]string{
		"p1234",
		"copenusage",
		"f9",
		"n/Users/test/.local/state/openusage/telemetry.sock",
	}, "\n")
	got := parseLSOFFirstRecord(out)
	want := "pid=1234 command=openusage socket=/Users/test/.local/state/openusage/telemetry.sock"
	if got != want {
		t.Fatalf("parseLSOFFirstRecord() = %q, want %q", got, want)
	}
}
