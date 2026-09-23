package activity

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRetainedCallLabelProvenance(t *testing.T) {
	for _, tc := range []struct{ name, source string }{{"generated", "tool"}, {"explicit-identical-label", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := New(root, Options{})
			if err != nil {
				t.Fatal(err)
			}
			// Decode the wire representation to cover old/new persisted-schema compatibility.
			events := []string{
				`{"kind":"call.created","call_id":"call_label","tool_name":"search_text","title":"Search text","activity_label_source":"tool"}`,
				`{"kind":"call.bound","call_id":"call_label","tool_name":"search_text","title":"搜索文本 · user query","activity_label":"Search text","activity_label_source":"` + tc.source + `"}`,
				`{"kind":"call.completed","call_id":"call_label","tool_name":"search_text","activity_label":"Search text","status":"succeeded"}`,
			}
			for _, raw := range events {
				var e Event
				if err := json.Unmarshal([]byte(raw), &e); err != nil {
					t.Fatal(err)
				}
				if _, err := store.Append(context.Background(), e); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := New(root, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range []*Store{store, reopened} {
				call, err := candidate.Call(context.Background(), "call_label")
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(call)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(data, &fields); err != nil {
					t.Fatal(err)
				}
				source, _ := fields["activity_label_source"].(string)
				if source != tc.source || call.Label != "Search text" || call.ToolName != "search_text" || call.Status != "succeeded" {
					t.Fatalf("retained label provenance or execution facts changed: %s", data)
				}
			}
		})
	}
}
