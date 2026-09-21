package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/tool/command/session"
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

func TestSessionObservePeekSchemaRejectsInvalidSelectorsAndKeepsLegacyBounds(t *testing.T) {
	schema, ok := InputSchema(ToolSessionObserve)
	if !ok {
		t.Fatal("missing session_observe schema")
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %#v", schema["additionalProperties"])
	}
	compiled, err := toolcontract.CompileInputSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	bounds := properties["max_output_bytes"].(map[string]any)
	if bounds["minimum"] != 1 || bounds["maximum"] != MaxOutputBytes {
		t.Fatalf("property bounds = %#v", bounds)
	}

	rejected := []map[string]any{
		{"action": "peek", "session_id": "s", "max_output_bytes": 1},
		{"action": "peek", "session_id": "s", "max_output_bytes": 3},
		{"action": "peek", "session_id": "s", "execution_request_id": "req"},
		{"action": "peek"},
		{"action": "peek", "session_id": "s", "stdout_offset": 1.5},
		{"action": "peek", "session_id": "s", "stdout_offset": -1},
		{"action": "peek", "session_id": "s", "stderr_offset": session.MaxSafeOutputOffset + 1},
		{"action": "list", "stdout_offset": 0},
		{"action": "status", "session_id": "s", "execution_request_id": "req"},
		{"action": "status"},
	}
	for _, args := range rejected {
		if err := compiled.Validate(args); err == nil {
			t.Fatalf("accepted invalid peek arguments %#v", args)
		}
	}
	accepted := []map[string]any{
		{"action": "peek", "session_id": "s"},
		{"action": "peek", "execution_request_id": "req", "stdout_offset": 0, "stderr_offset": 4, "max_output_bytes": 4},
		{"action": "status", "session_id": "s", "max_output_bytes": 1},
		{"action": "list"},
		{},
	}
	for _, args := range accepted {
		if err := compiled.Validate(args); err != nil {
			t.Fatalf("rejected valid arguments %#v: %v", args, err)
		}
	}
}

func TestSessionObservePeekRejectsInvalidArgumentsAndPreservesLegacyReads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	service, _ := newCommandTestService(t)
	negative := int64(-1)
	huge := session.MaxSafeOutputOffset + 1
	small := 1
	cases := []struct {
		name string
		args map[string]any
		code string
	}{
		{name: "both selectors", args: map[string]any{"action": "peek", "session_id": "s", "execution_request_id": "req"}, code: "INVALID_ARGUMENT"},
		{name: "no selector", args: map[string]any{"action": "peek"}, code: "INVALID_ARGUMENT"},
		{name: "max three", args: map[string]any{"action": "peek", "session_id": "s", "max_output_bytes": 3}, code: "INVALID_ARGUMENT"},
		{name: "negative offset", args: map[string]any{"action": "peek", "session_id": "s", "stdout_offset": negative}, code: "INVALID_OUTPUT_CURSOR"},
		{name: "huge offset", args: map[string]any{"action": "peek", "session_id": "s", "stderr_offset": huge}, code: "INVALID_OUTPUT_CURSOR"},
		{name: "list offset", args: map[string]any{"action": "list", "stdout_offset": int64(0)}, code: "INVALID_ARGUMENT"},
		{name: "status request id", args: map[string]any{"action": "status", "session_id": "s", "execution_request_id": "req"}, code: "INVALID_ARGUMENT"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.observeArgs(test.args)
			if code := toolCode(err); code != test.code {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}

	ready := filepath.Join(t.TempDir(), "ready")
	started, err := service.execArgs(context.Background(), map[string]any{
		"cmd":            fmt.Sprintf("while [ ! -f %q ]; do sleep 0.01; done; printf 'legacy-status'", ready),
		"execution_mode": "async",
		"timeout_ms":     5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started["session_id"].(string)
	stored, ok := service.sessions.Get(sessionID)
	if !ok {
		t.Fatal("async session was not stored")
	}
	if err = os.WriteFile(ready, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stored.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("command did not finish")
	}

	completedPeek, err := service.observeArgs(map[string]any{"action": "peek", "session_id": sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if completedPeek["stdout"] != "legacy-status" || completedPeek["command_ok"] != true || completedPeek["status"] != "exited" {
		t.Fatalf("completed peek = %#v", completedPeek)
	}
	repeatedPeek, err := service.observeArgs(map[string]any{"action": "peek", "session_id": sessionID, "stdout_offset": int64(0)})
	if err != nil {
		t.Fatal(err)
	}
	if repeatedPeek["stdout"] != "legacy-status" {
		t.Fatalf("repeat peek = %#v", repeatedPeek)
	}
	if _, stillStored := service.sessions.Get(sessionID); !stillStored {
		t.Fatal("peek consumed a completed legacy session")
	}

	status, err := service.observeArgs(map[string]any{"action": "status", "session_id": sessionID, "max_output_bytes": small})
	if err != nil {
		t.Fatal(err)
	}
	if status["stdout"] == "" {
		t.Fatalf("legacy status lost output: %#v", status)
	}
	if _, stillStored := service.sessions.Get(sessionID); stillStored {
		t.Fatal("legacy status left a completed session stored")
	}

	running, err := service.execArgs(context.Background(), map[string]any{
		"cmd":            "printf 'peek-me'; sleep 30",
		"execution_mode": "async",
		"timeout_ms":     60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runningID, _ := running["session_id"].(string)
	t.Cleanup(func() {
		_, _ = service.actArgs(map[string]any{"action": "kill", "session_id": runningID})
	})
	deadline := time.Now().Add(time.Second)
	var peeked Result
	for {
		peeked, err = service.observeArgs(map[string]any{"action": "peek", "session_id": runningID, "max_output_bytes": 4})
		if err != nil {
			t.Fatal(err)
		}
		if peeked["stdout"] == "peek" || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	again, err := service.observeArgs(map[string]any{"action": "peek", "session_id": runningID, "stdout_offset": int64(0), "max_output_bytes": 4})
	if err != nil {
		t.Fatal(err)
	}
	if peeked["stdout"] != "peek" || again["stdout"] != "peek" || again["stdout_next_offset"] != int64(4) {
		t.Fatalf("peek results = %#v %#v", peeked, again)
	}
	if _, exists := peeked["command_ok"]; exists {
		t.Fatalf("running peek reported command_ok: %#v", peeked)
	}
	if _, stillStored := service.sessions.Get(runningID); !stillStored {
		t.Fatal("peek consumed the running session")
	}
	_, err = service.observeArgs(map[string]any{"action": "peek", "session_id": runningID, "stdout_offset": int64(1 << 20)})
	if code := toolCode(err); code != "INVALID_OUTPUT_CURSOR" {
		t.Fatalf("future offset error = %v", err)
	}
	_, err = service.observeArgs(map[string]any{"action": "peek", "execution_request_id": "missing"})
	if code := toolCode(err); code != "EXECUTION_REQUEST_NOT_FOUND" {
		t.Fatalf("request id error = %v", err)
	}
}

func toolCode(err error) string {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Code
	}
	return ""
}
