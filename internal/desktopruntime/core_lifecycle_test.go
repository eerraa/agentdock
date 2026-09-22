package desktopruntime

import "testing"

func TestCommandArgsAreTunnelSupervisor(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "supervisor", args: []string{`C:\AgentDock\agentdock-core.exe`, "tunnel", "launch", "--runtime-root", `C:\AgentDock`}, want: true},
		{name: "quoted path still parsed", args: []string{`C:\Program Files\AgentDock\agentdock-core.exe`, "tunnel", "launch"}, want: true},
		{name: "flags before subcommand", args: []string{`core.exe`, "-test.run=Helper", "tunnel", "launch"}, want: true},
		{name: "http server", args: []string{`C:\AgentDock\agentdock-core.exe`, "service", "launch-core", "--runtime-root", `C:\AgentDock`}, want: false},
		{name: "tunnel start", args: []string{`C:\AgentDock\agentdock-core.exe`, "tunnel", "start"}, want: false},
		{name: "config update", args: []string{`C:\AgentDock\agentdock-core.exe`, "config", "update"}, want: false},
		{name: "executable only", args: []string{`C:\tunnel\launch\agentdock-core.exe`}, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := commandArgsAreTunnelSupervisor(test.args); got != test.want {
				t.Fatalf("commandArgsAreTunnelSupervisor(%q)=%v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestShouldRestorePublicTunnelOnlyWhenSupervisorIsGone(t *testing.T) {
	if shouldRestorePublicTunnel("named", 0) != true || shouldRestorePublicTunnel("quick", 0) != true {
		t.Fatal("named and quick tunnels must be restored when the supervisor pid is missing")
	}
	if shouldRestorePublicTunnel("named", 42) || shouldRestorePublicTunnel("quick", 7) {
		t.Fatal("a live supervisor must be left running across a core restart")
	}
	for _, mode := range []string{"none", "funnel", "local", ""} {
		if shouldRestorePublicTunnel(mode, 0) {
			t.Fatalf("mode %q must not start a cloudflared supervisor", mode)
		}
	}
	if !shouldRestorePublicTunnelAfterRestart("named", 10, 0) {
		t.Fatal("a supervisor removed by core restart must be restored")
	}
	if shouldRestorePublicTunnelAfterRestart("named", 0, 0) {
		t.Fatal("a tunnel that was already stopped must stay stopped")
	}
	if shouldRestorePublicTunnelAfterRestart("named", 10, 10) {
		t.Fatal("a supervisor that survived core restart must be left alone")
	}
}
