package activity

import (
	"os"
	"path/filepath"
	"testing"
)

// Keep numeric overflow rejection at the persisted-state boundary. A faster
// decoder must not wrap an out-of-range sequence into another valid identity.
func TestRetainedEventRejectsOverflowSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overflow.jsonl")
	data := "{\"schema_version\":1,\"seq\":20700000000000000000,\"kind\":\"call.created\"}\n" +
		"{\"schema_version\":1,\"seq\":18446744073709551616,\"kind\":\"call.created\"}\n" +
		"{\"schema_version\":1,\"seq\":3,\"kind\":\"call.created\"}\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var events []Event
	warning := false
	if err := scanEvents(path, func(event Event) bool { events = append(events, event); return true }, &warning); err != nil {
		t.Fatal(err)
	}
	if !warning || len(events) != 1 || events[0].Seq != 3 {
		t.Fatalf("overflowed journal sequence was accepted or following valid event was lost: warning=%v events=%+v", warning, events)
	}
}
