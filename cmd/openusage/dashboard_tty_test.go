package main

import (
	"errors"
	"testing"
)

func TestCheckDashboardTerminal(t *testing.T) {
	const stdinFd, stdoutFd = 10, 11

	tests := []struct {
		name    string
		ttys    map[int]bool
		wantErr bool
	}{
		{name: "both terminals", ttys: map[int]bool{stdinFd: true, stdoutFd: true}},
		{name: "stdout piped", ttys: map[int]bool{stdinFd: true}, wantErr: true},
		{name: "stdin redirected", ttys: map[int]bool{stdoutFd: true}, wantErr: true},
		{name: "neither terminal", ttys: map[int]bool{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isTerminal := func(fd int) bool { return tt.ttys[fd] }
			err := checkDashboardTerminal(isTerminal, stdinFd, stdoutFd)
			if tt.wantErr {
				if !errors.Is(err, errDashboardNeedsTerminal) {
					t.Fatalf("err = %v, want errDashboardNeedsTerminal", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
