package scripts

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

func TestControlPanelResourcesStayAlignedAcrossLocales(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel", "Resources")
	neutral := readResx(t, filepath.Join(root, "UiStrings.resx"))
	chinese := readResx(t, filepath.Join(root, "UiStrings.zh-CN.resx"))
	korean := readResx(t, filepath.Join(root, "UiStrings.ko-KR.resx"))
	if len(neutral) < 324 {
		t.Fatalf("neutral resources lost the original key set: %d", len(neutral))
	}
	if keysOf(neutral)[0] != "MainWindowTitle" || keysOf(neutral)[323] != "PublicOAuthCapabilitiesMissing" {
		t.Fatal("original resource key order changed")
	}
	if !sameKeys(neutral, chinese) || !sameKeys(neutral, korean) {
		t.Fatal("locale resource key sets diverged")
	}
	for _, key := range []string{"KoreanLanguage", "Activity_NeedsAttention", "Activity_State_running", "Activity_Kind_command", "Execution_GlyphPendingApproval", "Execution_ConversationTerminated"} {
		if _, ok := neutral[key]; !ok {
			t.Fatalf("missing resource %s", key)
		}
	}
	if neutral["KoreanLanguage"] != "한국어" || chinese["KoreanLanguage"] != "한국어" || korean["KoreanLanguage"] != "한국어" {
		t.Fatal("Korean language name must stay the native name in every locale")
	}
	for key, value := range neutral {
		if strings.HasPrefix(key, "\x00") {
			continue
		}
		if strings.TrimSpace(value) == "" || strings.TrimSpace(chinese[key]) == "" || strings.TrimSpace(korean[key]) == "" {
			t.Fatalf("empty translation for %s", key)
		}
		if formatTokens(value) != formatTokens(chinese[key]) || formatTokens(value) != formatTokens(korean[key]) {
			t.Fatalf("format placeholders diverged for %s", key)
		}
	}
}

func TestOwnerUIDoesNotBranchOnTranslatedText(t *testing.T) {
	root := filepath.Join("..", "..", "desktop", "windows", "control-panel")
	files := []string{
		"ExecutionWindow.xaml",
		"ExecutionWindow.xaml.cs",
		"ExecutionWindow.Actions.cs",
		"ExecutionDialogs.cs",
		filepath.Join("Models", "ExecutionModels.cs"),
		filepath.Join("Services", "ExecutionClient.cs"),
		"MainWindow.Access.cs",
		"App.Activity.cs",
	}
	for _, relative := range files {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		code := stripComments(string(data))
		for _, line := range strings.Split(code, "\n") {
			if strings.Contains(line, "LegacyPlaceholderTitle") {
				continue
			}
			for _, r := range line {
				if unicode.Is(unicode.Han, r) {
					t.Fatalf("translated control flow or display literal in %s: %s", relative, strings.TrimSpace(line))
				}
			}
		}
	}
	window, err := os.ReadFile(filepath.Join(root, "ExecutionWindow.xaml.cs"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(window)
	for _, want := range []string{"_detailsKind == DetailsKind.Connection", "_warningKind == WarningKind.ConversationTerminated"} {
		if !strings.Contains(source, want) && !strings.Contains(mustRead(t, filepath.Join(root, "ExecutionWindow.Actions.cs")), want) {
			t.Fatalf("execution window lost kind-based branch %s", want)
		}
	}
	if strings.Contains(source, `DetailsTitle.Text ==`) || strings.Contains(mustRead(t, filepath.Join(root, "ExecutionWindow.Actions.cs")), `DetailsTitle.Text ==`) {
		t.Fatal("details title text is still used as control flow")
	}
	models := mustRead(t, filepath.Join(root, "Models", "ExecutionModels.cs"))
	if !strings.Contains(models, `titleSource is "manual" or "host" or "task" or "operation"`) {
		t.Fatal("legacy title predicate must keep manual, host, task, and operation titles")
	}
}

func TestKoreanInstallerResourcesAreIndependent(t *testing.T) {
	definition := mustRead(t, filepath.Join("..", "..", "packaging", "windows", "AgentDock.iss"))
	for _, want := range []string{
		`UsePreviousLanguage=no`,
		`LanguageDetectionMethod=uilanguage`,
		`ShowLanguageDialog=no`,
		`Name: "english"`,
		`Name: "chinesesimplified"`,
		`Name: "korean"; MessagesFile: "compiler:Default.isl, languages\Korean.isl"`,
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("installer language contract missing %q", want)
		}
	}
	if strings.Contains(definition, `compiler:Languages\Korean.isl`) {
		t.Fatal("installer must not assume the compiler Korean language file")
	}
	messages := mustRead(t, filepath.Join("..", "..", "packaging", "windows", "includes", "messages.iss"))
	english := messageSet(t, messages, "english")
	chinese := messageSet(t, messages, "chinesesimplified")
	korean := messageSet(t, messages, "korean")
	if len(english) != len(chinese) || len(english) != len(korean) {
		t.Fatalf("custom message key counts diverged: en=%d zh=%d ko=%d", len(english), len(chinese), len(korean))
	}
	for key, value := range english {
		if _, ok := korean[key]; !ok {
			t.Fatalf("korean custom message missing %s", key)
		}
		if innoTokens(value) != innoTokens(korean[key]) || innoTokens(value) != innoTokens(chinese[key]) {
			t.Fatalf("installer placeholders diverged for %s", key)
		}
	}
	language, err := os.ReadFile(filepath.Join("..", "..", "packaging", "windows", "languages", "Korean.isl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytesContain(language, []byte{'\r'}) {
		t.Fatal("Korean.isl must use LF line endings")
	}
	text := string(language)
	for _, want := range []string{"is-6_4_1", "LanguageName=", "SetupAppTitle="} {
		if !strings.Contains(text, want) {
			t.Fatalf("Korean.isl missing %q", want)
		}
	}
	install := mustRead(t, filepath.Join("..", "..", "scripts", "install", "install.ps1"))
	if strings.Contains(definition, "ui-language") || strings.Contains(install, "ui-language") {
		t.Fatal("installer must not overwrite the WPF language preference")
	}
}

func readResx(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Data []struct {
			Name  string `xml:"name,attr"`
			Value string `xml:"value"`
		} `xml:"data"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(doc.Data))
	order := make([]string, 0, len(doc.Data))
	for _, item := range doc.Data {
		if _, exists := out[item.Name]; exists {
			t.Fatalf("duplicate resource %s in %s", item.Name, path)
		}
		out[item.Name] = item.Value
		order = append(order, item.Name)
	}
	out["\x00order"] = strings.Join(order, "\n")
	return out
}

func keysOf(resources map[string]string) []string {
	return strings.Split(resources["\x00order"], "\n")
}

func sameKeys(left, right map[string]string) bool {
	return left["\x00order"] == right["\x00order"] && len(left) == len(right)
}

func formatTokens(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if i+1 < len(value) && (value[i:i+2] == "{{" || value[i:i+2] == "}}") {
			i++
			continue
		}
		if value[i] != '{' {
			continue
		}
		end := strings.IndexByte(value[i:], '}')
		if end < 0 {
			return "UNCLOSED"
		}
		b.WriteString(value[i : i+end+1])
		b.WriteByte('\n')
		i += end
	}
	return b.String()
}

func stripComments(source string) string {
	source = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(source, "")
	return regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(source, "")
}

func messageSet(t *testing.T, messages, language string) map[string]string {
	t.Helper()
	out := map[string]string{}
	prefix := language + "."
	for _, line := range strings.Split(messages, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("custom message missing value: %s", line)
		}
		out[strings.TrimPrefix(key, prefix)] = value
	}
	return out
}

func innoTokens(value string) string {
	return strings.Join(regexp.MustCompile(`%[0-9n]|\{[A-Za-z0-9_]+\}`).FindAllString(value, -1), "\n")
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func bytesContain(data, needle []byte) bool {
	return strings.Contains(string(data), string(needle))
}
