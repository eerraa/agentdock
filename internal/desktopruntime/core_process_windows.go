//go:build windows

package desktopruntime

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func queryProcessCommandLine(processID uint32) (string, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)

	size := uint32(1024)
	var buf []byte
	for size <= 1<<20 {
		buf = make([]byte, size)
		var needed uint32
		err = windows.NtQueryInformationProcess(
			process,
			windows.ProcessCommandLineInformation,
			unsafe.Pointer(&buf[0]),
			size,
			&needed,
		)
		if err == nil {
			return commandLineFromProcessInfo(buf)
		}
		if needed <= size || needed > 1<<20 {
			return "", err
		}
		size = needed
	}
	return "", err
}

func commandLineFromProcessInfo(buf []byte) (string, error) {
	header := int(unsafe.Sizeof(windows.NTUnicodeString{}))
	if len(buf) < header {
		return "", fmt.Errorf("进程命令行信息长度不足: %d", len(buf))
	}
	value := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
	if value.Length == 0 {
		return "", nil
	}
	length := uintptr(value.Length)
	base := uintptr(unsafe.Pointer(&buf[0]))
	limit := base + uintptr(len(buf))
	raw := uintptr(unsafe.Pointer(value.Buffer))
	var ptr uintptr
	switch {
	case raw >= base && raw+length <= limit:
		ptr = raw
	case raw+length <= uintptr(len(buf)):
		ptr = base + raw
	default:
		return "", fmt.Errorf("进程命令行指针超出返回缓冲区")
	}
	chars := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), value.Length/2)
	return windows.UTF16ToString(chars), nil
}

func processIsTunnelSupervisor(processID uint32) (bool, error) {
	line, err := queryProcessCommandLine(processID)
	if err != nil {
		return false, err
	}
	args, err := windows.DecomposeCommandLine(line)
	if err != nil {
		return false, err
	}
	return commandArgsAreTunnelSupervisor(args), nil
}

// excludeTunnelSupervisors keeps every agentdock-core.exe whose command line is
// `tunnel launch`. A mutex miss must not make Core stop kill that process.
// Processes whose command line cannot be read are excluded only when the
// supervisor mutex identifies them. If both checks fail, stop refuses to guess.
func excludeTunnelSupervisors(runtimeRoot, binaryPath string, excluded map[uint32]struct{}) error {
	processIDs, err := processIDsAtPath(binaryPath)
	if err != nil {
		return err
	}
	supervisorPID, supervisorErr := activeTunnelSupervisorPID(runtimeRoot, binaryPath)
	var unidentified []uint32
	for _, processID := range processIDs {
		supervisor, lineErr := processIsTunnelSupervisor(processID)
		if lineErr != nil {
			unidentified = append(unidentified, processID)
			continue
		}
		if supervisor {
			excluded[processID] = struct{}{}
		}
	}
	if supervisorErr != nil && len(unidentified) > 0 {
		return fmt.Errorf("识别 Tunnel supervisor 失败: %w", supervisorErr)
	}
	if supervisorErr == nil && supervisorPID != 0 {
		excluded[supervisorPID] = struct{}{}
	}
	return nil
}

func coreStopTargets(runtimeRoot, binaryPath string) ([]uint32, error) {
	processIDs, err := processIDsAtPath(binaryPath)
	if err != nil {
		return nil, err
	}
	excluded := map[uint32]struct{}{}
	if err := excludeTunnelSupervisors(runtimeRoot, binaryPath, excluded); err != nil {
		return nil, err
	}
	ancestors, err := ancestorProcessIDsAtPath(binaryPath)
	if err != nil {
		return nil, err
	}
	for processID := range ancestors {
		excluded[processID] = struct{}{}
	}
	targets := make([]uint32, 0, len(processIDs))
	for _, processID := range processIDs {
		if _, skip := excluded[processID]; skip {
			continue
		}
		targets = append(targets, processID)
	}
	return targets, nil
}

func coreServerProcessRunning(runtimeRoot, binaryPath string) (bool, error) {
	processIDs, err := processIDsAtPath(binaryPath)
	if err != nil {
		return false, err
	}
	supervisorPID, supervisorErr := activeTunnelSupervisorPID(runtimeRoot, binaryPath)
	for _, processID := range processIDs {
		supervisor, lineErr := processIsTunnelSupervisor(processID)
		if lineErr == nil {
			if !supervisor {
				return true, nil
			}
			continue
		}
		if supervisorErr == nil && processID == supervisorPID {
			continue
		}
		return true, nil
	}
	return false, nil
}

func ensureCoreStopPermitted(runtimeRoot string, manifest Manifest) error {
	targets, err := coreStopTargets(runtimeRoot, ActiveCoreBinary(runtimeRoot, manifest))
	if err != nil {
		return err
	}
	for _, processID := range targets {
		handle, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE, false, processID)
		if openErr != nil {
			return fmt.Errorf("当前权限无法停止 AgentDock Core (PID %d)，已保留 Tunnel: %w", processID, openErr)
		}
		windows.CloseHandle(handle)
	}
	return nil
}

func restorePublicTunnelAfterCoreRestart(ctx context.Context, runtimeRoot string, supervisorBefore uint32) error {
	if supervisorBefore == 0 {
		return nil
	}
	runtime, err := loadTunnelRuntime(runtimeRoot)
	if err != nil {
		return err
	}
	supervisorPID, err := activeTunnelSupervisorPID(runtimeRoot, ActiveCoreBinary(runtimeRoot, runtime.manifest))
	if err != nil {
		return fmt.Errorf("识别 Tunnel supervisor 失败: %w", err)
	}
	if !shouldRestorePublicTunnelAfterRestart(runtime.mode, supervisorBefore, supervisorPID) {
		return nil
	}
	return startCloudflareTunnel(ctx, runtime)
}

func coreHealthy(ctx context.Context, runtimeRoot string) bool {
	manifest, _, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return false
	}
	return testHealth(ctx, manifest.HealthURL())
}

func waitCoreHealthy(ctx context.Context, runtimeRoot string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if coreHealthy(ctx, runtimeRoot) {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}
