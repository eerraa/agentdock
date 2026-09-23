package insertion

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInsertionRejectsInvalidStoreEnvelopeWithoutReplacingIt(t *testing.T) {
	cases := map[string]string{
		"null":           `null`,
		"empty-object":   `{}`,
		"missing-schema": `{"sequence":0,"items":[]}`,
		"missing-items":  `{"schema_version":1,"sequence":0}`,
		"null-items":     `{"schema_version":1,"sequence":0,"items":null}`,
		"future-schema":  `{"schema_version":2,"sequence":0,"items":[]}`,
		"unknown-field":  `{"schema_version":1,"sequence":0,"items":[],"future_queue":{"keep":true}}`,
		"trailing-json":  `{"schema_version":1,"sequence":0,"items":[]} {}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			s, now, target := fixture(t)
			path := filepath.Join(s.root, "queue.json")
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := New(s.root, "new_run", func() time.Time { return *now }); err == nil {
				t.Error("startup accepted an invalid existing queue")
			}
			if _, err := s.Add(context.Background(), target, "new_submission", "keep existing state"); err == nil {
				t.Error("write accepted an invalid existing queue")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != source {
				t.Error("invalid queue bytes were overwritten")
			}
		})
	}
}
