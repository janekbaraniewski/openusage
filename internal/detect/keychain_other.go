//go:build !darwin

package detect

// detectMacOSKeychainCredentials is a no-op on non-darwin platforms.
// Linux uses the Secret Service API and Windows uses Credential Manager;
// neither is consulted by the AI CLIs we currently support, so this stub
// simply returns. If/when we add Linux/Windows credential stores, replace
// this with platform-specific probes behind their own build tags.
func detectMacOSKeychainCredentials(_ *Result) {}
