package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWindowsUninstallDefaultsToPreservingUserData(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "includes", "code.iss"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	start := strings.Index(source, "function InitializeUninstall(): Boolean;")
	end := strings.Index(source[start:], "function GetUninstallParameters")
	if start < 0 || end < 0 {
		t.Fatal("uninstall confirmation owner moved; review its actual contract")
	}
	confirmation := source[start : start+end]
	for _, required := range []string{"PurgeState := False;", "if not UninstallSilent then"} {
		if !strings.Contains(confirmation, required) {
			t.Errorf("missing non-destructive default: %s", required)
		}
	}
	pattern := regexp.MustCompile(`(?s)PurgeState\s*:=\s*MsgBox\(\s*GetLocalizedMessage\('PurgeStateQuestion'\),\s*mbConfirmation,\s*MB_YESNO\s+or\s+MB_DEFBUTTON2\s*\)\s*=\s*IDYES;`)
	if !pattern.MatchString(confirmation) {
		t.Fatal("the destructive data-purge prompt must default to No and purge only on explicit Yes")
	}
}
