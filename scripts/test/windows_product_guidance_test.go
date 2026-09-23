package scripts

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWindowsTunnelGuidanceCoversStableDiagnosticCodes(t *testing.T) {
	root := filepath.Join("..", "..")
	resource, err := os.ReadFile(filepath.Join(root, "desktop", "windows", "control-panel", "Resources", "UiStrings.ko-KR.resx"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Entries []struct {
			Name  string `xml:"name,attr"`
			Value string `xml:"value"`
		} `xml:"data"`
	}
	if err := xml.Unmarshal(resource, &document); err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, entry := range document.Entries {
		keys[entry.Name] = entry.Value
	}
	paths, err := filepath.Glob(filepath.Join(root, "internal", "desktopruntime", "tailscale*.go"))
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`tailscaleProblem\("([a-z_]+)"`)
	checked := map[string]bool{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllSubmatch(data, -1) {
			code := string(match[1])
			checked[code] = true
			if keys["TunnelDiagnostic_"+code] == "" {
				t.Errorf("%s has no Korean product guidance (%s)", code, path)
			}
		}
	}
	if len(checked) < 40 {
		t.Fatalf("diagnostic inventory unexpectedly incomplete: %d", len(checked))
	}
}
