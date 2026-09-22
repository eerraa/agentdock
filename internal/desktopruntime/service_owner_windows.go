//go:build windows

package desktopruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	serviceOwnerPIDFile   = "service-owner.pid"
	tunnelOwnerStateFile  = "tunnel-owner.txt"
	coreReloadRequestFile = "core-reload.request"
	processStillActive    = 259
)

// ServiceOwnerRequest is the stable shim's service host. The shim stays outside
// the job. Core and the tunnel supervisor are siblings inside it, so ending the
// host cannot orphan cloudflared. A core reload replaces only the core child.
type ServiceOwnerRequest struct {
	RuntimeRoot       string
	CoreBinary        string
	CoreArgs          []string
	TunnelBinary      string
	TunnelArgs        []string
	ExtraEnv          []string
	CoreExtraEnv      []string
	TunnelExtraEnv    []string
	Poll              time.Duration
	TunnelStopTimeout time.Duration
	CoreReady         func(context.Context) bool
}

type serviceOwner struct {
	request               ServiceOwnerRequest
	root                  string
	job                   windows.Handle
	wake                  windows.Handle
	core                  *ownedProc
	tunnel                *ownedProc
	replacingCore         bool
	nextTunnelStart       time.Time
	cloudflaredSweepUntil time.Time
}

type ownedProc struct {
	pid     int
	done    <-chan struct{}
	kill    func()
	started time.Time
}

func (proc *ownedProc) exited() bool {
	if proc == nil {
		return true
	}
	select {
	case <-proc.done:
		return true
	default:
		return false
	}
}

func RunServiceOwner(ctx context.Context, request ServiceOwnerRequest) error {
	request, err := normalizeServiceOwnerRequest(request)
	if err != nil {
		return err
	}
	job, err := newKillOnCloseJob()
	if err != nil {
		return err
	}
	wake, err := openOwnerWakeEvent(request.RuntimeRoot)
	if err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	owner := &serviceOwner{
		request:               request,
		root:                  request.RuntimeRoot,
		job:                   job,
		wake:                  wake,
		cloudflaredSweepUntil: time.Now().Add(15 * time.Second),
	}
	if err := writeRuntimeText(filepath.Join(owner.root, serviceOwnerPIDFile), strconv.Itoa(os.Getpid())); err != nil {
		owner.closeHandles()
		return err
	}
	defer func() {
		_ = os.Remove(filepath.Join(owner.root, serviceOwnerPIDFile))
		owner.shutdown()
	}()
	err = owner.loop(ctx)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func normalizeServiceOwnerRequest(request ServiceOwnerRequest) (ServiceOwnerRequest, error) {
	root := strings.TrimSpace(request.RuntimeRoot)
	if root == "" {
		return request, errors.New("AgentDock 服务所有者缺少运行目录")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return request, fmt.Errorf("解析 AgentDock 运行目录失败: %w", err)
	}
	request.RuntimeRoot = abs
	if strings.TrimSpace(request.CoreBinary) == "" {
		return request, errors.New("AgentDock 服务所有者缺少 Core 程序")
	}
	if len(request.CoreArgs) == 0 {
		request.CoreArgs = []string{"service", "launch-core", "--runtime-root", abs}
	}
	if strings.TrimSpace(request.TunnelBinary) == "" {
		request.TunnelBinary = request.CoreBinary
	}
	if len(request.TunnelArgs) == 0 {
		request.TunnelArgs = []string{"tunnel", "launch", "--runtime-root", abs}
	}
	if request.Poll <= 0 {
		request.Poll = 500 * time.Millisecond
	}
	if request.TunnelStopTimeout <= 0 {
		request.TunnelStopTimeout = 5 * time.Second
	}
	return request, nil
}

func (owner *serviceOwner) loop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = windows.ResetEvent(owner.wake)
		if err := owner.reconcile(ctx); err != nil {
			return err
		}
		wait := owner.request.Poll.Milliseconds()
		if wait < 1 {
			wait = 1
		}
		if wait > 5000 {
			wait = 5000
		}
		_, _ = windows.WaitForSingleObject(owner.wake, uint32(wait))
	}
}

func (owner *serviceOwner) reconcile(ctx context.Context) error {
	reloadPath := filepath.Join(owner.root, coreReloadRequestFile)
	if _, err := os.Stat(reloadPath); err == nil {
		return owner.reloadCore(ctx)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if owner.core == nil {
		child, err := owner.startChild(owner.request.CoreBinary, owner.request.CoreArgs, owner.request.CoreExtraEnv)
		if err != nil {
			return err
		}
		owner.core = child
	} else if owner.core.exited() && !owner.replacingCore {
		return errors.New("AgentDock Core 已退出")
	}
	wanted, err := tunnelSupervisorWanted(owner.root)
	if err != nil {
		return err
	}
	if wanted {
		return owner.ensureTunnel()
	}
	return owner.stopTunnel(ctx)
}

func (owner *serviceOwner) reloadCore(ctx context.Context) error {
	owner.replacingCore = true
	defer func() { owner.replacingCore = false }()
	if owner.core != nil {
		owner.core.kill()
		select {
		case <-owner.core.done:
		case <-time.After(5 * time.Second):
		}
		owner.core = nil
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		child, err := owner.startChild(owner.request.CoreBinary, owner.request.CoreArgs, owner.request.CoreExtraEnv)
		if err != nil {
			return err
		}
		owner.core = child
		if owner.waitCoreReady(ctx, deadline) && !child.exited() {
			if err := os.Remove(filepath.Join(owner.root, coreReloadRequestFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
		child.kill()
		select {
		case <-child.done:
		case <-time.After(5 * time.Second):
		}
		owner.core = nil
		if time.Now().After(deadline) {
			return errors.New("AgentDock Core 重载后健康检查未通过")
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (owner *serviceOwner) waitCoreReady(ctx context.Context, deadline time.Time) bool {
	if owner.request.CoreReady != nil {
		return owner.core != nil && !owner.core.exited() && owner.request.CoreReady(ctx)
	}
	for {
		if owner.core == nil || owner.core.exited() || ctx.Err() != nil {
			return false
		}
		if coreHealthy(ctx, owner.root) {
			return true
		}
		if time.Now().After(deadline) {
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

func (owner *serviceOwner) ensureTunnel() error {
	if owner.tunnel != nil && !owner.tunnel.exited() {
		return nil
	}
	if owner.tunnel != nil && owner.tunnel.exited() {
		lived := time.Since(owner.tunnel.started)
		owner.tunnel = nil
		if lived < time.Second {
			owner.nextTunnelStart = time.Now().Add(time.Second)
		}
	}
	if !owner.nextTunnelStart.IsZero() && time.Now().Before(owner.nextTunnelStart) {
		return nil
	}
	foreign, unknown := owner.foreignSupervisor()
	if unknown || foreign != 0 {
		return nil
	}
	owner.killLeftoverCloudflared()
	child, err := owner.startChild(owner.request.TunnelBinary, owner.request.TunnelArgs, owner.request.TunnelExtraEnv)
	if err != nil {
		return err
	}
	owner.tunnel = child
	owner.nextTunnelStart = time.Time{}
	return nil
}

func (owner *serviceOwner) stopTunnel(ctx context.Context) error {
	foreign, unknown := owner.foreignSupervisor()
	hadForeign := !unknown && foreign != 0
	hadChild := owner.tunnel != nil && !owner.tunnel.exited()
	if !hadForeign && !hadChild {
		if owner.tunnel != nil && owner.tunnel.exited() {
			owner.tunnel = nil
		}
		if time.Now().Before(owner.cloudflaredSweepUntil) {
			owner.killLeftoverCloudflared()
		}
		return nil
	}
	owner.cloudflaredSweepUntil = time.Now().Add(15 * time.Second)
	if hadForeign {
		_ = signalTunnelSupervisorStop(owner.root)
		deadline := time.Now().Add(owner.request.TunnelStopTimeout)
		for processAlive(foreign) && time.Now().Before(deadline) {
			if err := ctx.Err(); err != nil {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
		if processAlive(foreign) {
			_ = terminateProcessIDs([]uint32{foreign})
		}
	}
	if hadChild {
		_ = signalTunnelSupervisorStop(owner.root)
		timer := time.NewTimer(owner.request.TunnelStopTimeout)
		select {
		case <-owner.tunnel.done:
			timer.Stop()
		case <-timer.C:
			owner.tunnel.kill()
			<-owner.tunnel.done
		case <-ctx.Done():
			timer.Stop()
			owner.tunnel.kill()
			return ctx.Err()
		}
	}
	owner.tunnel = nil
	owner.nextTunnelStart = time.Time{}
	owner.killLeftoverCloudflared()
	return nil
}

func (owner *serviceOwner) foreignSupervisor() (uint32, bool) {
	found, err := activeTunnelSupervisorPID(owner.root, owner.request.TunnelBinary)
	if err != nil {
		return 0, true
	}
	if found == 0 || (owner.tunnel != nil && int(found) == owner.tunnel.pid) {
		return 0, false
	}
	return found, false
}

func (owner *serviceOwner) startChild(binary string, args, extra []string) (*ownedProc, error) {
	command := exec.Command(binary, args...)
	command.Dir = owner.root
	command.Env = mergeEnv(append(append([]string{}, owner.request.ExtraEnv...), extra...))
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("启动 AgentDock 服务子进程失败: %w", err)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(command.Process.Pid))
	if err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return nil, fmt.Errorf("打开 AgentDock 服务子进程失败: %w", err)
	}
	assignErr := windows.AssignProcessToJobObject(owner.job, handle)
	_ = windows.CloseHandle(handle)
	if assignErr != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return nil, fmt.Errorf("把 AgentDock 服务子进程加入 Job 失败: %w", assignErr)
	}
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	return &ownedProc{
		pid:     command.Process.Pid,
		done:    done,
		started: time.Now(),
		kill:    func() { _ = command.Process.Kill() },
	}, nil
}

func (owner *serviceOwner) shutdown() {
	if owner.job != 0 {
		_ = windows.CloseHandle(owner.job)
		owner.job = 0
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && ((owner.core != nil && !owner.core.exited()) || (owner.tunnel != nil && !owner.tunnel.exited())) {
		time.Sleep(20 * time.Millisecond)
	}
	if owner.core != nil && !owner.core.exited() {
		owner.core.kill()
	}
	if owner.tunnel != nil && !owner.tunnel.exited() {
		owner.tunnel.kill()
	}
	owner.closeHandles()
}

func (owner *serviceOwner) closeHandles() {
	if owner.job != 0 {
		_ = windows.CloseHandle(owner.job)
		owner.job = 0
	}
	if owner.wake != 0 {
		_ = windows.CloseHandle(owner.wake)
		owner.wake = 0
	}
}

func (owner *serviceOwner) cloudflaredBinary() string {
	manifest, _, err := loadDesktopManifest(owner.root)
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(manifest.CloudflaredBinary)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

func (owner *serviceOwner) killLeftoverCloudflared() {
	path := owner.cloudflaredBinary()
	if path == "" {
		return
	}
	running, err := processRunningAtPath(path)
	if err != nil || !running {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = StopBinaryProcesses(ctx, path, 5*time.Second)
}

func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("创建 AgentDock 服务 Job 失败: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("配置 AgentDock 服务 Job 失败: %w", err)
	}
	return job, nil
}

func ownerWakeEventName(runtimeRoot string) string {
	root, err := filepath.Abs(runtimeRoot)
	if err != nil {
		root = runtimeRoot
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(root))))
	return `Local\AgentDock.Owner.wake.` + hex.EncodeToString(sum[:12])
}

func openOwnerWakeEvent(runtimeRoot string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(ownerWakeEventName(runtimeRoot))
	if err != nil {
		return 0, err
	}
	event, err := windows.CreateEvent(nil, 1, 0, name)
	if event == 0 {
		return 0, fmt.Errorf("创建 AgentDock 服务所有者唤醒事件失败: %w", err)
	}
	return event, nil
}

func signalOwnerWake(runtimeRoot string) error {
	event, err := openOwnerWakeEvent(runtimeRoot)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	if err := windows.SetEvent(event); err != nil {
		return fmt.Errorf("唤醒 AgentDock 服务所有者失败: %w", err)
	}
	return nil
}

func setTunnelDesired(runtimeRoot string, running bool) error {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return err
	}
	value := "stopped"
	if running {
		value = "running"
	}
	return writeRuntimeText(filepath.Join(root, tunnelOwnerStateFile), value)
}

func tunnelDesiredRunning(runtimeRoot string) (bool, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(filepath.Join(root, tunnelOwnerStateFile))
	if err == nil {
		switch strings.ToLower(strings.TrimSpace(string(data))) {
		case "running":
			return true, nil
		case "stopped":
			return false, nil
		default:
			return false, fmt.Errorf("无效的 Tunnel 所有者状态: %q", strings.TrimSpace(string(data)))
		}
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	mode, err := currentCloudflaredMode(root)
	if err != nil {
		return false, err
	}
	return mode == "quick" || mode == "named", nil
}

func tunnelSupervisorWanted(runtimeRoot string) (bool, error) {
	desired, err := tunnelDesiredRunning(runtimeRoot)
	if err != nil || !desired {
		return false, err
	}
	mode, err := currentCloudflaredMode(runtimeRoot)
	if err != nil {
		return false, err
	}
	return mode == "quick" || mode == "named", nil
}

func currentCloudflaredMode(runtimeRoot string) (string, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(root, "cloudflared-mode.txt"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	mode := strings.ToLower(strings.TrimSpace(string(data)))
	if mode == "" {
		manifest, _, loadErr := loadDesktopManifest(root)
		if loadErr != nil {
			return "none", nil
		}
		mode = strings.ToLower(strings.TrimSpace(manifest.TunnelMode))
	}
	if mode == "" {
		return "none", nil
	}
	return mode, nil
}

func requestOwnedCoreReload(ctx context.Context, runtimeRoot string) error {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return err
	}
	path := filepath.Join(root, coreReloadRequestFile)
	if err := writeRuntimeText(path, "reload"); err != nil {
		return err
	}
	if err := signalOwnerWake(root); err != nil {
		return err
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		running, err := serviceOwnerRunning(root)
		if err != nil {
			return err
		}
		if !running {
			return errors.New("AgentDock 服务所有者在重载 Core 前退出")
		}
		if time.Now().After(deadline) {
			return errors.New("AgentDock Core 重载超时")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func serviceOwnerRunning(runtimeRoot string) (bool, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(filepath.Join(root, serviceOwnerPIDFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	pid64, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
	if err != nil || pid64 == 0 {
		return false, nil
	}
	pid := uint32(pid64)
	if !processAlive(pid) {
		return false, nil
	}
	line, err := queryProcessCommandLine(pid)
	if err != nil || strings.TrimSpace(line) == "" {
		return false, nil
	}
	args, err := windows.DecomposeCommandLine(line)
	if err != nil {
		return false, nil
	}
	return commandArgsAreCoreOwner(args) && commandArgsMatchRuntimeRoot(args, root), nil
}

func mergeEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	keys := make(map[string]struct{}, len(extra))
	for _, item := range extra {
		name, _, ok := strings.Cut(item, "=")
		if ok && name != "" {
			keys[strings.ToUpper(name)] = struct{}{}
		}
	}
	merged := make([]string, 0, len(os.Environ())+len(extra))
	for _, item := range os.Environ() {
		name, _, ok := strings.Cut(item, "=")
		if ok {
			if _, drop := keys[strings.ToUpper(name)]; drop {
				continue
			}
		}
		merged = append(merged, item)
	}
	return append(merged, extra...)
}
