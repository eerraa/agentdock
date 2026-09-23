package client

import (
	"context"
	"os"
	"testing"
)

func TestManagedOverrideRejectsInvalidEnvelopeAndPreservesConnection(t *testing.T) {
	cases := map[string]string{
		"null":            `null`,
		"empty-object":    `{}`,
		"missing-schema":  `{"revision":0,"servers":{}}`,
		"missing-servers": `{"schema_version":1,"revision":0}`,
		"null-servers":    `{"schema_version":1,"revision":0,"servers":null}`,
		"future-schema":   `{"schema_version":2,"revision":0,"servers":{}}`,
		"unknown-field":   `{"schema_version":1,"revision":0,"servers":{},"future_override":{"keep":true}}`,
		"trailing-json":   `{"schema_version":1,"revision":0,"servers":{}} {}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			m, calls, _ := overrideFixture(t)
			summary, _, err := m.Refresh(context.Background(), "managed")
			if err != nil {
				t.Fatal(err)
			}
			previous := m.states["managed"]
			client := previous.client
			if err := os.WriteFile(m.overridePath, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := m.readOverrides(); err == nil {
				t.Error("invalid existing override decoded as defaults")
			}
			if _, err := m.Update(context.Background(), "managed", summary.Revision, "persistent", descriptionPatch("must not overwrite"), false); err == nil {
				t.Error("invalid existing override was writable")
			}
			after, err := os.ReadFile(m.overridePath)
			if err != nil || string(after) != source {
				t.Error("invalid override bytes were overwritten")
			}
			current := m.Snapshots([]string{"managed"})
			if m.states["managed"] != previous || previous.client != client || calls.Load() != 1 || len(current) != 1 || current[0].Revision != summary.Revision {
				t.Error("rejected override changed last-good connection or revision")
			}
		})
	}
}
