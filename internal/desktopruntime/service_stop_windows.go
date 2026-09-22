//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

type serviceStopPlan struct {
	endTask      func() error
	afterEndWait time.Duration
	owners       func() ([]uint32, error)
	canTerminate func(uint32) bool
	terminate    func([]uint32) error
	waitGone     func(context.Context, []uint32, time.Duration) error
	reapTunnel   func(context.Context) error
}

// runServiceStop ends the service host first. Tunnel processes are reaped only
// after that host is gone, so a permission failure cannot kill cloudflared and
// leave Core behind.
func runServiceStop(ctx context.Context, plan serviceStopPlan) error {
	if plan.endTask != nil {
		_ = plan.endTask()
		if plan.afterEndWait > 0 && plan.waitGone != nil && plan.owners != nil {
			owners, err := plan.owners()
			if err != nil {
				return err
			}
			if len(owners) > 0 {
				if err := plan.waitGone(ctx, owners, plan.afterEndWait); err != nil && ctx.Err() != nil {
					return ctx.Err()
				}
			}
		}
	}
	if plan.owners == nil {
		return errors.New("AgentDock 服务所有者查询缺失")
	}
	owners, err := plan.owners()
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		if plan.reapTunnel == nil {
			return nil
		}
		return plan.reapTunnel(ctx)
	}
	for _, pid := range owners {
		if plan.canTerminate != nil && !plan.canTerminate(pid) {
			return fmt.Errorf("当前权限无法停止 AgentDock 服务所有者 (PID %d)，已保留 Tunnel", pid)
		}
	}
	if plan.terminate != nil {
		if err := plan.terminate(owners); err != nil {
			return err
		}
	}
	if plan.waitGone != nil {
		if err := plan.waitGone(ctx, owners, 15*time.Second); err != nil {
			return err
		}
	}
	owners, err = plan.owners()
	if err != nil {
		return err
	}
	if len(owners) > 0 {
		return errors.New("AgentDock 服务所有者仍在运行")
	}
	if plan.reapTunnel == nil {
		return nil
	}
	return plan.reapTunnel(ctx)
}

func stopOwnedService(ctx context.Context, manifest Manifest, runtimeRoot string) error {
	plan := serviceStopPlan{
		owners: func() ([]uint32, error) {
			return serviceOwnerProcessIDs(runtimeRoot, manifest)
		},
		canTerminate: processCanTerminate,
		terminate:    terminateProcessIDs,
		waitGone: func(ctx context.Context, pids []uint32, timeout time.Duration) error {
			return waitPIDsGone(ctx, pids, timeout)
		},
		reapTunnel: func(ctx context.Context) error {
			return reapRuntimeTunnel(ctx, manifest, runtimeRoot)
		},
	}
	if manifest.UsesScheduledTask() {
		plan.endTask = func() error {
			return runScheduledTaskCommand(ctx, "/End", "/TN", scheduledTaskPath(manifest.AgentDockTaskName))
		}
		plan.afterEndWait = 8 * time.Second
	}
	return runServiceStop(ctx, plan)
}

func serviceOwnerProcessIDs(runtimeRoot string, manifest Manifest) ([]uint32, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return nil, err
	}
	var binaries []string
	if shim := strings.TrimSpace(manifest.AgentDockBinary); shim != "" {
		binaries = append(binaries, shim)
	}
	core := ActiveCoreBinary(root, manifest)
	if core != "" && !samePath(core, manifest.AgentDockBinary) {
		binaries = append(binaries, core)
	}
	var ids []uint32
	for _, binary := range binaries {
		if strings.TrimSpace(binary) == "" {
			continue
		}
		ancestors, err := ancestorProcessIDsAtPath(binary)
		if err != nil {
			return nil, err
		}
		pids, err := processIDsAtPath(binary)
		if err != nil {
			return nil, err
		}
		for _, pid := range pids {
			if _, skip := ancestors[pid]; skip {
				continue
			}
			line, lineErr := queryProcessCommandLine(pid)
			if lineErr != nil || strings.TrimSpace(line) == "" {
				ids = append(ids, pid)
				continue
			}
			args, parseErr := windows.DecomposeCommandLine(line)
			if parseErr != nil {
				ids = append(ids, pid)
				continue
			}
			if commandArgsAreTunnelSupervisor(args) {
				continue
			}
			if commandArgsAreCoreOwner(args) && commandArgsMatchRuntimeRoot(args, root) {
				ids = append(ids, pid)
			}
		}
	}
	return uniquePIDs(ids), nil
}

func reapRuntimeTunnel(ctx context.Context, manifest Manifest, runtimeRoot string) error {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return err
	}
	coreBinary := ActiveCoreBinary(root, manifest)
	pids, err := processIDsAtPath(coreBinary)
	if err != nil {
		return err
	}
	var supervisors []uint32
	for _, pid := range pids {
		if processMatchesRuntimeCommand(pid, root, commandArgsAreTunnelSupervisor) {
			supervisors = append(supervisors, pid)
		}
	}
	shim := strings.TrimSpace(manifest.AgentDockBinary)
	if shim != "" && !samePath(shim, coreBinary) {
		shimPIDs, err := processIDsAtPath(shim)
		if err != nil {
			return err
		}
		for _, pid := range shimPIDs {
			if processMatchesRuntimeCommand(pid, root, commandArgsAreTunnelSupervisor) {
				supervisors = append(supervisors, pid)
			}
		}
	}
	if err := terminateProcessIDs(uniquePIDs(supervisors)); err != nil {
		return err
	}
	cloudflared := strings.TrimSpace(manifest.CloudflaredBinary)
	if cloudflared == "" {
		return nil
	}
	info, err := os.Stat(cloudflared)
	if err != nil || info.IsDir() {
		return nil
	}
	if err := StopBinaryProcesses(ctx, cloudflared, 15*time.Second); err != nil {
		return fmt.Errorf("停止 cloudflared 失败: %w", err)
	}
	return nil
}

func processMatchesRuntimeCommand(pid uint32, root string, match func([]string) bool) bool {
	line, err := queryProcessCommandLine(pid)
	if err != nil || strings.TrimSpace(line) == "" {
		return false
	}
	args, err := windows.DecomposeCommandLine(line)
	if err != nil {
		return false
	}
	return match(args) && commandArgsMatchRuntimeRoot(args, root)
}

func processCanTerminate(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}

func terminateProcessIDs(pids []uint32) error {
	pids = uniquePIDs(pids)
	if len(pids) == 0 {
		return nil
	}
	handles := make([]windows.Handle, 0, len(pids))
	closeAll := func() {
		for _, handle := range handles {
			_ = windows.CloseHandle(handle)
		}
	}
	for _, pid := range pids {
		handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
		if err != nil {
			closeAll()
			return fmt.Errorf("当前权限无法停止 AgentDock 服务所有者 (PID %d)，已保留 Tunnel: %w", pid, err)
		}
		handles = append(handles, handle)
	}
	var failures []string
	for index, handle := range handles {
		if err := windows.TerminateProcess(handle, 1); err != nil {
			failures = append(failures, fmt.Sprintf("PID %d: %v", pids[index], err))
		}
	}
	for _, handle := range handles {
		_, _ = windows.WaitForSingleObject(handle, 5000)
		_ = windows.CloseHandle(handle)
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func waitPIDsGone(ctx context.Context, pids []uint32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		alive := false
		for _, pid := range pids {
			if processAlive(pid) {
				alive = true
				break
			}
		}
		if !alive {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("AgentDock 服务所有者未在 %s 内退出", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func processAlive(pid uint32) bool {
	if pid == 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == processStillActive
}

func uniquePIDs(pids []uint32) []uint32 {
	seen := make(map[uint32]struct{}, len(pids))
	out := make([]uint32, 0, len(pids))
	for _, pid := range pids {
		if pid == 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		out = append(out, pid)
	}
	return out
}
