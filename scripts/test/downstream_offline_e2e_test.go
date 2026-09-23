package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownstreamInstallerCIRunsOfflinePayloadInStandardUser(t *testing.T) {
	read := func(parts ...string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	workflow := read(".github", "workflows", "windows-installer.yml")
	prepare := strings.Index(workflow, "- name: Prepare offline AMD64 payload")
	component := strings.Index(workflow, "- name: Download and verify cloudflared compatibility payload")
	test := strings.Index(workflow, "- name: Run full install and in-place upgrade as standard user")
	if prepare < 0 || component < prepare || test < component {
		t.Fatal("offline inputs must be prepared and verified before standard-user E2E")
	}
	for _, required := range []string{"-OfflineArchive $env:AMD64_ARCHIVE", "-OfflineChecksumFile $env:AMD64_CHECKSUM", "-OfflineCloudflaredBinary $env:AMD64_CLOUDFLARED", "prepare-bundled-rg.ps1 -Destination .\\dist\\tools\\rg", ".\\dist\\wsl-helper, .\\dist\\tools", "2837888cc0f5d58f15b6dc478376de90b4d3ba5241c7947455d1e0a0df429712", "SignatureStatus]::Valid"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("missing CI input verification: %q", required)
		}
	}
	child := read("scripts", "test", "test-install-windows-e2e.ps1")
	launcher := read("scripts", "test", "run-windows-installer-e2e-as-standard-user.ps1")
	for _, name := range []string{"OfflineArchive", "OfflineChecksumFile", "OfflineCloudflaredBinary"} {
		if strings.Count(child, "-"+name+" $"+name+" `") != 2 {
			t.Errorf("both clean and upgrade calls must receive %s", name)
		}
		if !strings.Contains(launcher, "-"+name+" `\"$copied") {
			t.Errorf("standard-user child lost %s", name)
		}
	}
	if !strings.Contains(child, "service stop --runtime-root $testRoot") {
		t.Fatal("fixture generation is not stopped through its runtime owner")
	}
}

func TestStandardUserInstallerRunnerPreservesWindowsPowerShellEncoding(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "test", "run-windows-installer-e2e-as-standard-user.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "\xef\xbb\xbf") {
		t.Fatal("the non-ASCII Windows PowerShell 5.1 E2E runner requires a UTF-8 BOM")
	}
}
