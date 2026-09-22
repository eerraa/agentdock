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

// shouldRestorePublicTunnel reports that a quick or named tunnel has lost the
// supervisor that owns cloudflared. Local and Tailscale funnel modes are left
// untouched.
func shouldRestorePublicTunnel(mode string, supervisorPID uint32) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "quick", "named":
		return supervisorPID == 0
	default:
		return false
	}
}

// shouldRestorePublicTunnelAfterRestart only replaces a supervisor that this
// core restart itself removed. A tunnel the user already stopped stays stopped.
func shouldRestorePublicTunnelAfterRestart(mode string, supervisorBefore, supervisorAfter uint32) bool {
	return supervisorBefore != 0 && shouldRestorePublicTunnel(mode, supervisorAfter)
}
