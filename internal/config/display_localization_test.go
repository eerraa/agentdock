package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayWarningCodesDoNotChangePersistedSchema(t *testing.T) {
	home := t.TempDir()
	s := NewDisplayPreferences(home, true)
	good := s.Snapshot()
	if good.WarningCode != "" || good.WarningDetail != "" {
		t.Fatal("fresh preferences contain failure metadata")
	}
	disabled := false
	if _, err := s.Update(t.Context(), DisplayChange{ExpectedRevision: 1, ChatGPTMCPUIEnabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "display-settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "warning") {
		t.Fatal("transient warning data was persisted")
	}
	if restored := NewDisplayPreferences(home, true).Snapshot(); restored.Revision != 2 || restored.ChatGPTMCPUIEnabled || restored.WarningCode != "" {
		t.Fatalf("round trip changed: %#v", restored)
	}
	const original = "invalid original preference data"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	broken := NewDisplayPreferences(home, true).Snapshot()
	if broken.WarningCode != "display_preferences_load_failed" || broken.WarningDetail == "" || broken.Warning == "" || broken.ChatGPTMCPUIEnabled {
		t.Fatalf("invalid coded failure: %#v", broken)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != original {
		t.Fatal("original preferences changed")
	}
}
