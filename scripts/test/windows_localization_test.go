package scripts

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestWindowsKoreanResourceContract(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	read := func(name string) map[string]string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "Resources", name))
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Values []struct {
				Name  string `xml:"name,attr"`
				Value string `xml:"value"`
			} `xml:"data"`
		}
		if err := xml.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		values := make(map[string]string, len(document.Values))
		for _, value := range document.Values {
			if _, duplicate := values[value.Name]; duplicate || strings.TrimSpace(value.Value) == "" {
				t.Fatalf("%s has duplicate/empty key %s", name, value.Name)
			}
			values[value.Name] = value.Value
		}
		return values
	}
	english := read("UiStrings.resx")
	// Match composite-format items, not literal JSON examples in help text.
	placeholder := regexp.MustCompile(`\{[0-9]+[^{}]*\}`)
	for _, name := range []string{"UiStrings.zh-CN.resx", "UiStrings.ko-KR.resx"} {
		localized := read(name)
		if len(localized) != len(english) {
			t.Fatalf("%s key count=%d, expected %d", name, len(localized), len(english))
		}
		for key, source := range english {
			value, exists := localized[key]
			if !exists {
				t.Errorf("%s missing %s", name, key)
				continue
			}
			want, got := placeholder.FindAllString(source, -1), placeholder.FindAllString(value, -1)
			sort.Strings(want)
			sort.Strings(got)
			if !reflect.DeepEqual(want, got) {
				t.Errorf("%s/%s format items differ: got %q, want %q", name, key, got, want)
			}
		}
	}
	korean := read("UiStrings.ko-KR.resx")
	for key, want := range map[string]string{"MainWindowTitle": "AgentDock 제어판", "KoreanLanguage": "한국어", "DisplaySettings": "표시"} {
		if korean[key] != want {
			t.Errorf("Korean %s=%q, want %q", key, korean[key], want)
		}
	}
	markup, err := os.ReadFile(filepath.Join(root, "MainWindow.xaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"system", "en", "zh-CN", "ko-KR"} {
		if !strings.Contains(string(markup), `Tag="`+tag+`"`) {
			t.Errorf("missing language choice %s", tag)
		}
	}
	for _, key := range []string{"DisplaySettings", "Theme", "EmbeddedInterface", "ProvideEmbeddedInterface", "EmbeddedInterfaceHelp"} {
		if !strings.Contains(string(markup), "{local:Loc "+key+"}") {
			t.Errorf("display UI does not use the existing localization mechanism: %s", key)
		}
	}
}
