//go:build !darwin && !linux

package daemon

import (
	"fmt"
	"runtime"
)

func (m ServiceManager) Install() error {
	return fmt.Errorf("daemon service install is unsupported on %s", runtime.GOOS)
}

func (m ServiceManager) Uninstall() error {
	return fmt.Errorf("daemon service uninstall is unsupported on %s", runtime.GOOS)
}

func (m ServiceManager) Start() error {
	return fmt.Errorf("daemon service start is unsupported on %s", runtime.GOOS)
}
