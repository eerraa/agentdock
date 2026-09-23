package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/activity"
)

func TestExecutionGeneratedLabelProvenance(t *testing.T) {
	r := executionTestRuntime(t)
	ctx := activity.WithSource(context.Background(), activity.Source{Principal: "test", Namespace: "mcp", HostConversationID: "generated-labels"})
	if err := os.WriteFile(filepath.Join(r.cfg.AgentDockDefaultDir, "label.txt"), []byte("preserved content"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, label, source string }{
		{"generated", "", "tool"},
		{"explicit-same-as-default", "Edit file", ""},
		{"explicit-user-text", "한국어 사용자 中文", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"action": "replace", "path": "label.txt", "old": "preserved", "new": "retained", "dry_run": true}
			if tc.label != "" {
				args["activity_label"] = tc.label
			}
			result, err := r.Call(ctx, "file_edit", args)
			if err != nil {
				t.Fatal(err)
			}
			call, err := r.activity.Call(ctx, stringArg(result, "call_id"))
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(call)
			if err != nil {
				t.Fatal(err)
			}
			var view map[string]any
			if err := json.Unmarshal(data, &view); err != nil {
				t.Fatal(err)
			}
			if source := stringArg(view, "activity_label_source"); source != tc.source {
				t.Fatalf("label provenance=%q want=%q; view=%s", source, tc.source, data)
			}
			want := tc.label
			if want == "" {
				want = "Edit file"
			}
			if call.Label != want || call.ToolName != "file_edit" || call.Status != "succeeded" {
				t.Fatalf("localization metadata changed original text, tool or outcome: %+v", call)
			}
			if _, accepted := result["activity_label_source"]; accepted {
				t.Fatal("presentation provenance leaked into business arguments")
			}
		})
	}
	if _, err := r.Call(ctx, "file_edit", map[string]any{"action": "replace", "path": "label.txt", "old": "preserved", "new": "retained", "dry_run": true, "activity_label_source": "tool"}); err == nil {
		t.Fatal("client supplied a server-owned label source")
	}
}
