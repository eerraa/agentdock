//go:build !windows

package command

import (
	"runtime"
	"testing"
)

func TestNonWindowsExecCommandRejectsRuntimeOverride(t *testing.T) {
	service, _ := newCommandTestService(t)
	_, err := service.prepareCommandInvocationArgs(map[string]any{"runtime": "wsl"}, "pwd")
	if err == nil {
		t.Fatal("expected non-Windows runtime override to be rejected")
	}
}

func TestNonWindowsCommandMetadataUsesResolvedHostWorkdir(t *testing.T) {
	service, _ := newCommandTestService(t)
	invocation, err := service.prepareCommandInvocation(ExecRequest{Cmd: "printf fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if invocation.execution.Runtime != runtime.GOOS || invocation.workdir == "" || invocation.execution.Workdir != invocation.workdir {
		t.Fatalf("missing resolved host execution metadata: %#v", invocation)
	}
	if invocation.execution.Distribution != "" {
		t.Fatal("native host command acquired WSL metadata")
	}
}
