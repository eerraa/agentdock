//go:build windows

package desktopruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func coreServerMutexName(runtimeRoot string) (string, error) {
	root, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(root))))
	return "Local\\AgentDock.Core." + hex.EncodeToString(sum[:12]), nil
}

// HoldCoreServer pins one launch-core process to the runtime root.
// release must run on the same goroutine. A second process exits with
// ErrCoreAlreadyServing once /healthz answers, and otherwise reports that the
// owner is still starting or stuck.
func HoldCoreServer(ctx context.Context, runtimeRoot string) (func(), error) {
	goruntime.LockOSThread()
	releaseThread := func() { goruntime.UnlockOSThread() }

	name, err := coreServerMutexName(runtimeRoot)
	if err != nil {
		releaseThread()
		return nil, err
	}
	mutexName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		releaseThread()
		return nil, err
	}
	mutex, createErr := windows.CreateMutex(nil, false, mutexName)
	if mutex == 0 {
		releaseThread()
		return nil, fmt.Errorf("创建 Core 单实例 mutex 失败: %w", createErr)
	}
	result, waitErr := windows.WaitForSingleObject(mutex, 0)
	if waitErr != nil {
		windows.CloseHandle(mutex)
		releaseThread()
		return nil, fmt.Errorf("检查 Core 单实例 mutex 失败: %w", waitErr)
	}
	switch result {
	case uint32(windows.WAIT_OBJECT_0), uint32(windows.WAIT_ABANDONED):
		if coreHealthy(ctx, runtimeRoot) {
			_ = windows.ReleaseMutex(mutex)
			windows.CloseHandle(mutex)
			releaseThread()
			return nil, ErrCoreAlreadyServing
		}
		return func() {
			_ = windows.ReleaseMutex(mutex)
			windows.CloseHandle(mutex)
			releaseThread()
		}, nil
	case uint32(windows.WAIT_TIMEOUT):
		windows.CloseHandle(mutex)
		releaseThread()
		if waitCoreHealthy(ctx, runtimeRoot, 3*time.Second) {
			return nil, ErrCoreAlreadyServing
		}
		return nil, errors.New("AgentDock Core 已在运行，但健康检查未通过")
	default:
		windows.CloseHandle(mutex)
		releaseThread()
		return nil, fmt.Errorf("检查 Core 单实例 mutex 返回未知状态: 0x%x", result)
	}
}
