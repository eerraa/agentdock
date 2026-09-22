//go:build windows

package file

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var searchRipgrepExecutable = os.Executable

func defaultInspectBundledRipgrep() (string, bundleStatus) {
	return inspectBundledRipgrepAt(runtime.GOARCH, searchRipgrepExecutable)
}

func inspectBundledRipgrepAt(goarch string, executable func() (string, error)) (string, bundleStatus) {
	if !bundledRipgrepSupported(runtime.GOOS, goarch) {
		return "", bundleAbsent
	}
	if executable == nil {
		return "", bundleAbsent
	}
	exe, err := executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "", bundleAbsent
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}
	return evaluateBundledRipgrep(filepath.Join(filepath.Dir(exe), filepath.FromSlash(bundledRipgrepPayloadDir), "rg.exe"))
}
