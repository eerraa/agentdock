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

func TestCommandArgsAreCoreOwner(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "shim", args: []string{`C:\AgentDock\agentdock.exe`, "service", "launch-core", "--runtime-root", `C:\AgentDock`}, want: true},
		{name: "flags before subcommand", args: []string{`core.exe`, "-test.run=Helper", "service", "launch-core"}, want: true},
		{name: "tunnel supervisor", args: []string{`C:\AgentDock\agentdock-core.exe`, "tunnel", "launch", "--runtime-root", `C:\AgentDock`}, want: false},
		{name: "service stop", args: []string{`C:\AgentDock\agentdock.exe`, "service", "stop"}, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := commandArgsAreCoreOwner(test.args); got != test.want {
				t.Fatalf("commandArgsAreCoreOwner(%q)=%v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestCommandArgsMatchRuntimeRoot(t *testing.T) {
	args := []string{`agentdock.exe`, "service", "launch-core", "--runtime-root", `C:\AgentDock`}
	if !commandArgsMatchRuntimeRoot(args, `c:\agentdock`) {
		t.Fatal("expected case-insensitive runtime root match")
	}
	if commandArgsMatchRuntimeRoot(args, `C:\Other`) {
		t.Fatal("a different runtime root must not match")
	}
	if !commandArgsMatchRuntimeRoot([]string{`agentdock.exe`, "service", "launch-core"}, `C:\AgentDock`) {
		t.Fatal("a command without --runtime-root stays install-scoped by executable path")
	}
}
