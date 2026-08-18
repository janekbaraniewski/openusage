package claude_code

import (
	"errors"
	"os/exec"
	"testing"
)

func TestKeychainItemNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "security item missing",
			err:  &exec.ExitError{Stderr: []byte("security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.")},
			want: true,
		},
		{
			name: "generic item missing",
			err:  &exec.ExitError{Stderr: []byte("item not found")},
			want: true,
		},
		{
			name: "permission denied",
			err:  &exec.ExitError{Stderr: []byte("security: User interaction is not allowed.")},
		},
		{
			name: "command unavailable",
			err:  errors.New("executable file not found in $PATH"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keychainItemNotFound(tt.err); got != tt.want {
				t.Fatalf("keychainItemNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}
