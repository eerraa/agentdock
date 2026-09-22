package desktopruntime

import (
	"errors"
	"strings"
)

// ErrCoreAlreadyServing means another launch-core process is already accepting
// the runtime health check. The duplicate process must exit without binding.
var ErrCoreAlreadyServing = errors.New("AgentDock core is already serving")

// commandArgsAreTunnelSupervisor reports the Windows tunnel supervisor command,
// which is the same agentdock-core.exe image as the HTTP server:
//
//	agentdock-core.exe tunnel launch --runtime-root <dir>
//
// argv[0] is the executable. A later consecutive pair is accepted so tests and
// quoted paths can place flags before the subcommand.
func commandArgsAreTunnelSupervisor(args []string) bool {
	for index := 1; index < len(args)-1; index++ {
		if strings.EqualFold(args[index], "tunnel") && strings.EqualFold(args[index+1], "launch") {
			return true
		}
	}
	return false
}

// commandArgsAreCoreOwner reports the long-lived service host or the HTTP
// core it supervises. Both are `service launch-core`; the tunnel supervisor is not.
func commandArgsAreCoreOwner(args []string) bool {
	for index := 1; index < len(args)-1; index++ {
		if strings.EqualFold(args[index], "service") && strings.EqualFold(args[index+1], "launch-core") {
			return true
		}
	}
	return false
}

// commandArgsMatchRuntimeRoot is true when args name this install, or when they
// do not carry a runtime root. A different --runtime-root belongs to another install.
func commandArgsMatchRuntimeRoot(args []string, root string) bool {
	for index := 1; index < len(args)-1; index++ {
		if !strings.EqualFold(args[index], "--runtime-root") {
			continue
		}
		return samePath(args[index+1], root)
	}
	return true
}
