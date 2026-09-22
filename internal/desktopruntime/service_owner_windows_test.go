//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServiceOwnerStartsChildrenAndJobClosesOnProcessDeath(t *testing.T) {
	root := t.TempDir()
	prepareOwnerRuntime(t, root)
	coreFile := filepath.Join(root, "core.pid")
	tunnelFile := filepath.Join(root, "tunnel.pid")
	command := exec.Command(os.Args[0], "-test.run=^TestServiceOwnerJobHelper$")
	command.Env = append(os.Environ(),
		"AGENTDOCK_TEST_OWNER_PARENT=1",
		"AGENTDOCK_TEST_OWNER_ROOT="+root,
		"AGENTDOCK_TEST_OWNER_CORE_PID="+coreFile,
		"AGENTDOCK_TEST_OWNER_TUNNEL_PID="+tunnelFile,
	)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil && command.Process != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})

	corePID := waitForPIDFile(t, coreFile)
	tunnelPID := waitForPIDFile(t, tunnelFile)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = command.Process.Wait()
	waitUntilDead(t, corePID)
	waitUntilDead(t, tunnelPID)
}

func TestServiceOwnerStopsTunnelWithoutStoppingCore(t *testing.T) {
	root := t.TempDir()
	prepareOwnerRuntime(t, root)
	coreFile := filepath.Join(root, "core.pid")
	tunnelFile := filepath.Join(root, "tunnel.pid")
	ctx, stop := startTestOwner(t, root, coreFile, tunnelFile)
	corePID := waitForPIDFile(t, coreFile)
	tunnelPID := waitForPIDFile(t, tunnelFile)

	if err := setTunnelDesired(root, false); err != nil {
		t.Fatal(err)
	}
	if err := signalOwnerWake(root); err != nil {
		t.Fatal(err)
	}
	waitUntilDead(t, tunnelPID)
	if !processAlive(uint32(corePID)) {
		t.Fatal("core exited when only the tunnel was stopped")
	}
	stop()
	<-ctx
}

func TestServiceOwnerReloadReplacesCoreAndKeepsTunnel(t *testing.T) {
	root := t.TempDir()
	prepareOwnerRuntime(t, root)
	coreFile := filepath.Join(root, "core.pid")
	tunnelFile := filepath.Join(root, "tunnel.pid")
	_, stop := startTestOwner(t, root, coreFile, tunnelFile)
	defer stop()
	corePID := waitForPIDFile(t, coreFile)
	tunnelPID := waitForPIDFile(t, tunnelFile)

	if err := writeRuntimeText(filepath.Join(root, coreReloadRequestFile), "reload"); err != nil {
		t.Fatal(err)
	}
	if err := signalOwnerWake(root); err != nil {
		t.Fatal(err)
	}
	newCore := waitForPIDChange(t, coreFile, corePID)
	if !processAlive(uint32(tunnelPID)) {
		t.Fatal("tunnel exited during core reload")
	}
	if newCore == tunnelPID {
		t.Fatalf("reloaded core pid %d reused the tunnel pid", newCore)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, coreReloadRequestFile)); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("core reload request was not consumed")
}

func TestServiceOwnerExitsWhenCoreExits(t *testing.T) {
	root := t.TempDir()
	prepareOwnerRuntime(t, root)
	coreFile := filepath.Join(root, "core.pid")
	tunnelFile := filepath.Join(root, "tunnel.pid")
	errCh, stop := startTestOwner(t, root, coreFile, tunnelFile)
	corePID := waitForPIDFile(t, coreFile)
	tunnelPID := waitForPIDFile(t, tunnelFile)
	process, err := os.FindProcess(corePID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "AgentDock Core 已退出") {
			t.Fatalf("owner error = %v", err)
		}
	case <-time.After(5 * time.Second):
		stop()
		t.Fatal("owner did not exit after the core child exited")
	}
	waitUntilDead(t, tunnelPID)
}

func TestRunServiceStopKeepsTunnelWhenOwnerCannotBeTerminated(t *testing.T) {
	reaped := false
	terminated := false
	err := runServiceStop(context.Background(), serviceStopPlan{
		owners:       func() ([]uint32, error) { return []uint32{42}, nil },
		canTerminate: func(uint32) bool { return false },
		terminate: func([]uint32) error {
			terminated = true
			return nil
		},
		waitGone: func(context.Context, []uint32, time.Duration) error { return nil },
		reapTunnel: func(context.Context) error {
			reaped = true
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected permission error")
	}
	if terminated || reaped {
		t.Fatalf("terminated=%v reaped=%v", terminated, reaped)
	}
}

func TestRunServiceStopReapsTunnelOnlyAfterOwnerIsGone(t *testing.T) {
	terminated := false
	reaped := false
	err := runServiceStop(context.Background(), serviceStopPlan{
		owners: func() ([]uint32, error) {
			if terminated {
				return nil, nil
			}
			return []uint32{7}, nil
		},
		canTerminate: func(uint32) bool { return true },
		terminate: func([]uint32) error {
			terminated = true
			return nil
		},
		waitGone: func(context.Context, []uint32, time.Duration) error { return nil },
		reapTunnel: func(context.Context) error {
			if !terminated {
				t.Fatal("reaped tunnel before the owner was terminated")
			}
			reaped = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reaped {
		t.Fatal("tunnel was not reaped after the owner exited")
	}
}

func TestRunServiceStopReapsAfterTaskEndWithoutTerminate(t *testing.T) {
	present := true
	terminated := false
	reaped := false
	err := runServiceStop(context.Background(), serviceStopPlan{
		endTask: func() error {
			present = false
			return nil
		},
		afterEndWait: time.Second,
		owners: func() ([]uint32, error) {
			if present {
				return []uint32{3}, nil
			}
			return nil, nil
		},
		canTerminate: func(uint32) bool { return false },
		terminate: func([]uint32) error {
			terminated = true
			return nil
		},
		waitGone:   func(context.Context, []uint32, time.Duration) error { return nil },
		reapTunnel: func(context.Context) error { reaped = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if terminated || !reaped {
		t.Fatalf("terminated=%v reaped=%v", terminated, reaped)
	}
}

func TestTunnelDesiredStoppedWinsOverNamedMode(t *testing.T) {
	root := t.TempDir()
	if err := writeRuntimeText(filepath.Join(root, "cloudflared-mode.txt"), "named"); err != nil {
		t.Fatal(err)
	}
	wanted, err := tunnelSupervisorWanted(root)
	if err != nil || !wanted {
		t.Fatalf("missing desired file with named mode = %v, %v", wanted, err)
	}
	if err := setTunnelDesired(root, false); err != nil {
		t.Fatal(err)
	}
	wanted, err = tunnelSupervisorWanted(root)
	if err != nil || wanted {
		t.Fatalf("explicit stop = %v, %v", wanted, err)
	}
	if err := setTunnelDesired(root, true); err != nil {
		t.Fatal(err)
	}
	wanted, err = tunnelSupervisorWanted(root)
	if err != nil || !wanted {
		t.Fatalf("explicit run = %v, %v", wanted, err)
	}
	if err := writeRuntimeText(filepath.Join(root, "cloudflared-mode.txt"), "none"); err != nil {
		t.Fatal(err)
	}
	wanted, err = tunnelSupervisorWanted(root)
	if err != nil || wanted {
		t.Fatalf("named desired file must not start cloudflared when mode is none: %v, %v", wanted, err)
	}
}

func TestServiceHostBinaryPrefersExistingShim(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "agentdock.exe")
	if err := os.WriteFile(shim, []byte("shim"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := serviceHostBinary(Manifest{AgentDockBinary: shim}, root)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(got, shim) {
		t.Fatalf("service host = %s, want %s", got, shim)
	}
	if _, err := serviceHostBinary(Manifest{AgentDockBinary: filepath.Join(root, "missing.exe")}, root); err == nil {
		t.Fatal("expected missing service host to fail")
	}
}

func TestServiceOwnerHelper(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_OWNER_HELPER") != "1" {
		t.Skip("helper process only")
	}
	if path := os.Getenv("AGENTDOCK_TEST_OWNER_PIDFILE"); path != "" {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(2 * time.Minute)
}

func TestServiceOwnerJobHelper(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_OWNER_PARENT") != "1" {
		t.Skip("helper process only")
	}
	root := os.Getenv("AGENTDOCK_TEST_OWNER_ROOT")
	err := RunServiceOwner(context.Background(), testOwnerRequest(
		root,
		os.Getenv("AGENTDOCK_TEST_OWNER_CORE_PID"),
		os.Getenv("AGENTDOCK_TEST_OWNER_TUNNEL_PID"),
	))
	if err != nil {
		t.Fatal(err)
	}
}

func prepareOwnerRuntime(t *testing.T, root string) {
	t.Helper()
	if err := writeRuntimeText(filepath.Join(root, "cloudflared-mode.txt"), "named"); err != nil {
		t.Fatal(err)
	}
	if err := setTunnelDesired(root, true); err != nil {
		t.Fatal(err)
	}
}

func testOwnerRequest(root, corePID, tunnelPID string) ServiceOwnerRequest {
	return ServiceOwnerRequest{
		RuntimeRoot:       root,
		CoreBinary:        os.Args[0],
		CoreArgs:          []string{"-test.run=^TestServiceOwnerHelper$"},
		TunnelBinary:      os.Args[0],
		TunnelArgs:        []string{"-test.run=^TestServiceOwnerHelper$"},
		ExtraEnv:          []string{"AGENTDOCK_TEST_OWNER_HELPER=1"},
		CoreExtraEnv:      []string{"AGENTDOCK_TEST_OWNER_PIDFILE=" + corePID},
		TunnelExtraEnv:    []string{"AGENTDOCK_TEST_OWNER_PIDFILE=" + tunnelPID},
		Poll:              40 * time.Millisecond,
		TunnelStopTimeout: 200 * time.Millisecond,
		CoreReady:         func(context.Context) bool { return true },
	}
}

func startTestOwner(t *testing.T, root, corePID, tunnelPID string) (<-chan error, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		errCh <- RunServiceOwner(ctx, testOwnerRequest(root, corePID, tunnelPID))
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
		}
	})
	return errCh, cancel
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			last = strings.TrimSpace(string(data))
			pid, convErr := strconv.Atoi(last)
			if convErr == nil && pid > 0 && processAlive(uint32(pid)) {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %s (last %q)", path, last)
	return 0
}

func waitForPIDChange(t *testing.T, path string, old int) int {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 && pid != old && processAlive(uint32(pid)) {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s did not change from %d", path, old)
	return 0
}

func waitUntilDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(uint32(pid)) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d still running", pid)
}
