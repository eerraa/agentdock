//go:build !windows

package command

import (
	"runtime"
	"strings"

	"github.com/uvwt/agentdock/internal/tool/command/session"
)

func (svc *Service) prepareCommandInvocation(request ExecRequest) (commandInvocation, error) {
	if runtimeName := strings.TrimSpace(request.Runtime); runtimeName != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "runtime is only supported by AgentDock on Windows", "validation")
	}
	if distribution := strings.TrimSpace(request.WSLDistribution); distribution != "" {
		return commandInvocation{}, toolError("INVALID_ARGUMENT", "wsl_distribution is only supported by AgentDock on Windows", "validation")
	}
	invocation, err := svc.newHostCommandInvocation(request)
	if err != nil {
		return commandInvocation{}, err
	}
	invocation.execution = session.ExecutionContext{Runtime: runtime.GOOS, Workdir: invocation.workdir}
	return invocation, nil
}

func AddRuntimeProperties(_ map[string]any) {}

func WorkdirDescription() string {
	return "Host working directory. Relative paths resolve from ~/AgentDock."
}

func Description() string {
	return "Run a bounded command. Bind an active Skill with skill to use its installed root and isolated environment for this command; explicit workdir and env values override those defaults."
}
