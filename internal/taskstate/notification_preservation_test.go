package taskstate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompletionNotificationRejectsInvalidClaimEnvelopeWithoutReplay(t *testing.T) {
	cases := map[string]string{
		"null":           `null`,
		"empty-object":   `{}`,
		"missing-schema": `{"claimed":{}}`,
		"missing-claims": `{"schema_version":1}`,
		"null-claims":    `{"schema_version":1,"claimed":null}`,
		"future-schema":  `{"schema_version":2,"claimed":{}}`,
		"unknown-field":  `{"schema_version":1,"claimed":{},"future_claims":{"keep":true}}`,
		"trailing-json":  `{"schema_version":1,"claimed":{}} {}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			task := completedNotificationTask(t, s)
			now := task.CompletedAt.Add(time.Second)
			if items, err := s.ClaimCompletionNotifications(context.Background(), 3, now); err != nil || len(items) != 1 {
				t.Fatalf("initial notification: %v %v", items, err)
			}
			path := filepath.Join(s.root, "notification-claims.json")
			primary := filepath.Join(s.root, task.ID+".json")
			before, err := os.ReadFile(primary)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if items, err := s.ClaimCompletionNotifications(context.Background(), 3, now); err == nil || len(items) != 0 {
				t.Errorf("invalid claim store permitted notification replay: %v %v", items, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != source {
				t.Error("invalid claim store bytes were overwritten")
			}
			after, err = os.ReadFile(primary)
			if err != nil || string(after) != string(before) {
				t.Error("notification read changed canonical task state")
			}
		})
	}
}
