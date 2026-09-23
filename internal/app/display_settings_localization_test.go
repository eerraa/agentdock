package app

import (
	"github.com/uvwt/agentdock/internal/config"
	"testing"
)

func TestDisplayGuidanceHasStablePresentationCodes(t *testing.T) {
	settings := config.DisplaySettings{SchemaVersion: 1, Revision: 7, Warning: "legacy diagnostic", WarningCode: "display_preferences_load_failed", WarningDetail: "original I/O detail"}
	result := displayResult(settings)
	for key, expected := range map[string]any{"revision": uint64(7), "chatgpt_mcp_ui_enabled": false, "warning": settings.Warning, "warning_code": settings.WarningCode, "warning_detail": settings.WarningDetail, "refresh_hint_code": "refresh_chatgpt_connection", "host_adoption": "unknown", "server_policy_applied": true} {
		if result[key] != expected {
			t.Errorf("%s: got %#v; want %#v", key, result[key], expected)
		}
	}
	if result["refresh_hint"] == "" {
		t.Fatal("legacy clients lost their guidance")
	}
}
