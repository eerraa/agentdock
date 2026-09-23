package scripts

import (
	"encoding/xml"
	"github.com/uvwt/agentdock/internal/app"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryRegisteredToolHasLocalizedGeneratedLabel(t *testing.T) {
	for _, file := range []string{"UiStrings.resx", "UiStrings.ko-KR.resx", "UiStrings.zh-CN.resx"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "desktop", "windows", "control-panel", "Resources", file))
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Data []struct {
				Name  string `xml:"name,attr"`
				Value string `xml:"value"`
			} `xml:"data"`
		}
		if err := xml.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		values := map[string]string{}
		for _, item := range document.Data {
			if _, exists := values[item.Name]; exists {
				t.Fatalf("duplicate resource %s in %s", item.Name, file)
			}
			values[item.Name] = item.Value
		}
		for _, spec := range app.ToolDefinitions() {
			key := "ExecutionTool_" + spec.Name
			translated, exists := values[key]
			if !exists || strings.TrimSpace(translated) == "" {
				t.Errorf("%s has no generated label for registered tool %s", file, spec.Name)
			}
			if file == "UiStrings.resx" && translated != spec.Title {
				t.Errorf("English default changed for %s: %q != %q", spec.Name, translated, spec.Title)
			}
		}
	}
}
