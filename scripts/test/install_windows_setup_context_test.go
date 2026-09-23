package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsSetupContextProbeIncludesOfflinePayload(t *testing.T) {
	data, err := os.ReadFile("run-windows-installer-e2e-as-standard-user.ps1")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(data), "\r\n", "\n")
	start := strings.Index(source, "$contextArguments =")
	end := strings.Index(source, "$contextProcess =")
	if start < 0 || end <= start {
		t.Fatal("Setup context probe argument boundary is missing")
	}
	for _, flag := range []string{"-OfflineArchive", "-OfflineChecksumFile", "-OfflineCloudflaredBinary"} {
		if !strings.Contains(source[start:end], flag) {
			t.Errorf("Setup user-context probe must reach the ownership guard using its verified payload; missing %s", flag)
		}
	}
	if !strings.Contains(source, "$contextCode -ne 'Code=setup-elevated-context'") || !strings.Contains(source, "$contextSuccess -ne 'Success=false'") {
		t.Fatal("offline inputs must not replace the failed wrong-user ownership assertion")
	}
}
