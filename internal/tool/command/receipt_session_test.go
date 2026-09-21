package command

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestReceiptSessionSurvivesStatusAndKillForLaterPeek(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	service, _ := newCommandTestService(t)
	started, err := service.execArgs(context.Background(), map[string]any{
		"cmd":                  "printf 'kept-output'",
		"execution_mode":       "async",
		"timeout_ms":           2000,
		"execution_request_id": "0123456789abcdef0123456789abcdef.0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started["session_id"].(string)
	stored, ok := service.sessions.Get(sessionID)
	if !ok || !stored.Recoverable() {
		t.Fatal("receipt session was not retained")
	}
	select {
	case <-stored.Done:
	case <-time.After(time.Second):
		t.Fatal("command did not finish")
	}
	status, err := service.observeArgs(map[string]any{"action": "status", "session_id": sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if status["stdout"] != "kept-output" || status["command_ok"] != true {
		t.Fatalf("status = %#v", status)
	}
	if _, stillStored := service.sessions.Get(sessionID); !stillStored {
		t.Fatal("status deleted a receipt session")
	}
	peeked, err := service.observeArgs(map[string]any{"action": "peek", "session_id": sessionID, "stdout_offset": int64(0)})
	if err != nil {
		t.Fatal(err)
	}
	if peeked["stdout"] != "kept-output" || peeked["stdout_next_offset"] != int64(len("kept-output")) {
		t.Fatalf("peek after status = %#v", peeked)
	}
	if _, err = service.actArgs(map[string]any{"action": "kill", "session_id": sessionID}); err != nil {
		t.Fatal(err)
	}
	if _, stillStored := service.sessions.Get(sessionID); !stillStored {
		t.Fatal("kill deleted a completed receipt session")
	}
	again, err := service.observeArgs(map[string]any{"action": "peek", "session_id": sessionID})
	if err != nil || again["stdout"] != "kept-output" {
		t.Fatalf("peek after kill = %#v %v", again, err)
	}
}
