package app

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/uvwt/agentdock/internal/activity"
)

func TestSessionObservePeekUsesExistingSessionOwnership(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	r := newRuntimeValidationTestRuntime(t)
	owner := activity.WithSource(context.Background(), activity.Source{Principal: "peek-owner", Namespace: "mcp", HostConversationID: "chat-owner"})
	other := activity.WithSource(context.Background(), activity.Source{Principal: "peek-other", Namespace: "mcp", HostConversationID: "chat-other"})
	started, err := r.Call(owner, "exec_command", map[string]any{
		"cmd":            "sleep 30",
		"execution_mode": "async",
		"timeout_ms":     60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := stringArg(started, "session_id")
	if sessionID == "" {
		t.Fatalf("missing session: %#v", started)
	}
	t.Cleanup(func() {
		_, _ = r.Call(owner, "session_act", map[string]any{"action": "kill", "session_id": sessionID})
	})

	peeked, err := r.Call(owner, "session_observe", map[string]any{"action": "peek", "session_id": sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if peeked["session_id"] != sessionID || peeked["status"] != "running" {
		t.Fatalf("owner peek = %#v", peeked)
	}
	if _, exists := peeked["command_ok"]; exists {
		t.Fatalf("running peek reported command_ok: %#v", peeked)
	}
	if peeked["call_id"] == "" || peeked["call_id"] == started["call_id"] {
		t.Fatalf("peek call_id = %#v, exec call_id = %#v", peeked["call_id"], started["call_id"])
	}

	_, err = r.Call(other, "session_observe", map[string]any{"action": "peek", "session_id": sessionID})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "SESSION_OWNER_MISMATCH" {
		t.Fatalf("other owner error = %v", err)
	}
	still, err := r.Call(owner, "session_observe", map[string]any{"action": "peek", "session_id": sessionID, "stdout_offset": 0})
	if err != nil {
		t.Fatal(err)
	}
	if still["session_id"] != sessionID {
		t.Fatalf("owner peek changed after rejection: %#v", still)
	}
}
