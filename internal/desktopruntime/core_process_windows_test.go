//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestQueryProcessCommandLineReadsCurrentProcess(t *testing.T) {
	line, err := queryProcessCommandLine(uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	args, err := windows.DecomposeCommandLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) == 0 {
		t.Fatalf("empty command line: %q", line)
	}
	sameBinary := samePath(args[0], mustAbs(t, os.Args[0])) || strings.EqualFold(filepath.Base(args[0]), filepath.Base(os.Args[0]))
	if !sameBinary {
		t.Fatalf("command line did not identify this process: %q", line)
	}
	if commandArgsAreTunnelSupervisor(args) {
		t.Fatalf("test process was classified as the tunnel supervisor: %q", line)
	}
}

func TestCoreStopTargetsIgnoreTunnelLaunch(t *testing.T) {
	root := t.TempDir()
	writeRuntimeManifest(t, root, 8765)
	supervisor := startCoreProcessHelper(t, "tunnel", "launch", "--runtime-root", root)
	server := startCoreProcessHelper(t, "service", "launch-core", "--runtime-root", root)
	waitForProcessCommand(t, supervisor)
	waitForProcessCommand(t, server)

	targets, err := coreStopTargets(root, mustAbs(t, os.Args[0]))
	if err != nil {
		t.Fatal(err)
	}
	if containsPID(targets, uint32(supervisor.Process.Pid)) {
		line, _ := queryProcessCommandLine(uint32(supervisor.Process.Pid))
		t.Fatalf("tunnel launch PID %d was a core stop target: %q", supervisor.Process.Pid, line)
	}
	if !containsPID(targets, uint32(server.Process.Pid)) {
		line, _ := queryProcessCommandLine(uint32(server.Process.Pid))
		t.Fatalf("launch-core PID %d was not a core stop target: %q", server.Process.Pid, line)
	}
	manifest, _, err := loadDesktopManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureCoreStopPermitted(root, manifest); err != nil {
		t.Fatal(err)
	}
}

func TestHoldCoreServerRejectsDuplicateUntilHealthy(t *testing.T) {
	root := t.TempDir()
	release, err := HoldCoreServer(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	result := filepath.Join(root, "result.txt")
	command := exec.Command(os.Args[0], "-test.run=^TestHoldCoreServerHelper$")
	command.Env = append(os.Environ(),
		"AGENTDOCK_TEST_CORE_LOCK_HELPER=1",
		"AGENTDOCK_TEST_CORE_ROOT="+root,
		"AGENTDOCK_TEST_CORE_RESULT="+result,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, output)
	}
	body, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "busy" {
		t.Fatalf("duplicate without health=%q, want busy", body)
	}
}

func TestHoldCoreServerReportsAlreadyServing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	host, portText, err := net.SplitHostPort(mustURLHost(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if host == "::1" {
		host = "127.0.0.1"
	}

	root := t.TempDir()
	release, err := HoldCoreServer(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	writeRuntimeManifest(t, root, port, host)

	result := filepath.Join(root, "result.txt")
	command := exec.Command(os.Args[0], "-test.run=^TestHoldCoreServerHelper$")
	command.Env = append(os.Environ(),
		"AGENTDOCK_TEST_CORE_LOCK_HELPER=1",
		"AGENTDOCK_TEST_CORE_ROOT="+root,
		"AGENTDOCK_TEST_CORE_RESULT="+result,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, output)
	}
	body, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "already" {
		t.Fatalf("healthy duplicate=%q, want already", body)
	}
}

func TestHoldCoreServerDoesNotClaimALivePort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	host, portText, err := net.SplitHostPort(mustURLHost(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if host == "::1" {
		host = "127.0.0.1"
	}
	root := t.TempDir()
	writeRuntimeManifest(t, root, port, host)

	_, err = HoldCoreServer(context.Background(), root)
	if !errors.Is(err, ErrCoreAlreadyServing) {
		t.Fatalf("live port must refuse a new owner, got %v", err)
	}

	result := filepath.Join(root, "result.txt")
	command := exec.Command(os.Args[0], "-test.run=^TestHoldCoreServerHelper$")
	command.Env = append(os.Environ(),
		"AGENTDOCK_TEST_CORE_LOCK_HELPER=1",
		"AGENTDOCK_TEST_CORE_ROOT="+root,
		"AGENTDOCK_TEST_CORE_RESULT="+result,
	)
	started := time.Now()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, output)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("duplicate waited as if the core mutex was still held")
	}
	body, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "already" {
		t.Fatalf("second process=%q, want already", body)
	}
}

func TestHoldCoreServerHelper(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_CORE_LOCK_HELPER") != "1" {
		t.Skip("helper process only")
	}
	root := os.Getenv("AGENTDOCK_TEST_CORE_ROOT")
	result := os.Getenv("AGENTDOCK_TEST_CORE_RESULT")
	_, err := HoldCoreServer(context.Background(), root)
	text := "ok"
	switch {
	case errors.Is(err, ErrCoreAlreadyServing):
		text = "already"
	case err != nil && strings.Contains(err.Error(), "已在运行"):
		text = "busy"
	case err != nil:
		text = err.Error()
	}
	if writeErr := os.WriteFile(result, []byte(text), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
}

func TestCoreProcessHelper(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_CORE_PROCESS_HELPER") != "1" {
		t.Skip("helper process only")
	}
	time.Sleep(30 * time.Second)
}

func startCoreProcessHelper(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	commandArgs := append([]string{"-test.run=^TestCoreProcessHelper$"}, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), "AGENTDOCK_TEST_CORE_PROCESS_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.Process != nil && command.ProcessState == nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})
	return command
}

func waitForProcessCommand(t *testing.T, command *exec.Cmd) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if command.ProcessState != nil {
			t.Fatalf("helper exited before its command line could be read")
		}
		line, err := queryProcessCommandLine(uint32(command.Process.Pid))
		if err == nil && line != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for helper command line")
}

func writeRuntimeManifest(t *testing.T, root string, port int, host ...string) {
	t.Helper()
	listenHost := "127.0.0.1"
	if len(host) > 0 && host[0] != "" {
		listenHost = host[0]
	}
	binary := mustAbs(t, os.Args[0])
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		AgentDockBinary: binary,
		Host:            listenHost,
		Port:            port,
		LocalMCPURL:     fmt.Sprintf("http://%s:%d/mcp", listenHost, port),
		TunnelMode:      "none",
		InstallChannel:  "setup",
	}
	if err := Save(filepath.Join(root, "runtime.json"), manifest); err != nil {
		t.Fatal(err)
	}
}

func containsPID(processIDs []uint32, want uint32) bool {
	for _, processID := range processIDs {
		if processID == want {
			return true
		}
	}
	return false
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

func mustURLHost(t *testing.T, raw string) string {
	t.Helper()
	if index := strings.Index(raw, "://"); index >= 0 {
		raw = raw[index+3:]
	}
	return raw
}
