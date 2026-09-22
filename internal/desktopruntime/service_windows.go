//go:build windows

package desktopruntime

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const windowsCoreStartTimeout = 2 * time.Minute

func platformServiceStatus(ctx context.Context, runtimeRoot string) (ServiceStatus, error) {
	manifest, _, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return ServiceStatus{}, err
	}
	coreBinary := ActiveCoreBinary(runtimeRoot, manifest)
	running, err := coreServerProcessRunning(runtimeRoot, coreBinary)
	if err != nil {
		return ServiceStatus{}, err
	}
	healthy := testHealth(ctx, manifest.HealthURL())
	startupEnabled, err := coreAutostartEnabled(ctx, manifest)
	if err != nil {
		return ServiceStatus{}, fmt.Errorf("读取 AgentDock 开机启动状态失败: %w", err)
	}
	return ServiceStatus{Running: running || healthy, Healthy: healthy, StartupEnabled: startupEnabled}, nil
}

func platformServiceAction(ctx context.Context, runtimeRoot, action string) error {
	manifest, root, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return err
	}

	switch action {
	case "start":
		return startCore(ctx, manifest, root)
	case "stop":
		return stopCore(ctx, manifest, root)
	case "restart":
		// 所有者进程结束后 Job 会带走 Core 和 cloudflared。新所有者读取
		// tunnel-owner.txt，因此用户已经停下的 Tunnel 不会被重新拉起。
		if err := stopCore(ctx, manifest, root); err != nil {
			return err
		}
		return startCore(ctx, manifest, root)
	default:
		return fmt.Errorf("不支持的 Windows 服务操作：%s", action)
	}
}

func loadDesktopManifest(runtimeRoot string) (Manifest, string, error) {
	root, err := filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return Manifest{}, "", fmt.Errorf("解析 Windows 运行目录失败: %w", err)
	}
	manifest, err := Load(filepath.Join(root, "runtime.json"))
	if err != nil {
		return Manifest{}, "", fmt.Errorf("读取 Windows 运行清单失败: %w", err)
	}
	return manifest, root, nil
}

func startCore(ctx context.Context, manifest Manifest, runtimeRoot string) error {
	if err := CheckExecutionCompatibility(ctx, runtimeRoot, ActiveCoreBinary(runtimeRoot, manifest)); err != nil {
		return err
	}
	if testHealth(ctx, manifest.HealthURL()) {
		return nil
	}
	recoverAbandonedCoreLocks(manifest, runtimeRoot)
	if manifest.UsesScheduledTask() {
		if err := StartInteractiveScheduledTask(ctx, runtimeRoot, manifest.AgentDockTaskName); err != nil {
			return err
		}
	} else if err := startDetachedCore(manifest, runtimeRoot); err != nil {
		return err
	}
	return waitForHealth(ctx, manifest.HealthURL(), windowsCoreStartTimeout)
}

func stopCore(ctx context.Context, manifest Manifest, runtimeRoot string) error {
	if err := stopOwnedService(ctx, manifest, runtimeRoot); err != nil {
		return fmt.Errorf("停止 AgentDock 核心失败: %w", err)
	}
	return nil
}

func serviceHostBinary(manifest Manifest, runtimeRoot string) (string, error) {
	shim := strings.TrimSpace(manifest.AgentDockBinary)
	if shim != "" {
		if info, err := os.Stat(shim); err == nil && !info.IsDir() {
			return shim, nil
		}
	}
	coreBinary := ActiveCoreBinary(runtimeRoot, manifest)
	if info, err := os.Stat(coreBinary); err != nil || info.IsDir() {
		if shim != "" {
			return "", fmt.Errorf("找不到 AgentDock 核心程序: %s", shim)
		}
		return "", fmt.Errorf("找不到 AgentDock 核心程序: %s", coreBinary)
	}
	return coreBinary, nil
}

func startDetachedCore(manifest Manifest, runtimeRoot string) error {
	// 标准权限也走稳定 shim。shim 才是 Job 所有者；直接拉起 generation Core 会让 Tunnel 再次脱离。
	binary, err := serviceHostBinary(manifest, runtimeRoot)
	if err != nil {
		return err
	}
	command := exec.Command(binary, "service", "launch-core", "--runtime-root", runtimeRoot)
	// launch-core 会自行把运行日志写入受限轮转文件；父进程不再持有同一路径的追加句柄。
	command.Dir = manifest.AgentDockDefaultDir
	if command.Dir == "" {
		command.Dir = defaultWindowsWorkDir()
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动 AgentDock 核心失败: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("释放 AgentDock 后台进程句柄失败: %w", err)
	}
	return nil
}

func defaultWindowsWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	workDir := filepath.Join(home, "AgentDock")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return home
	}
	return workDir
}

func waitForHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if testHealth(ctx, url) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("AgentDock 健康检查失败: %s", url)
}

func testHealth(ctx context.Context, url string) bool {
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}
