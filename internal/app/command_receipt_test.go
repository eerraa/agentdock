package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/activity"
	"github.com/uvwt/agentdock/internal/permission"
)

func receiptCaller(principal, hostConversation string) context.Context {
	return activity.WithSource(context.Background(), activity.Source{Principal: principal, Provider: "openai", Namespace: "mcp", HostConversationID: hostConversation})
}

func newExecutionRequestID(r *Runtime) string {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(err)
	}
	return r.executionEpoch() + "." + hex.EncodeToString(nonce[:])
}

func receiptToolCode(err error) string {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Code
	}
	return ""
}

func effectCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(data)
}

func TestExecReceiptDeduplicatesLostResponsesAndKeepsOwnership(t *testing.T) {
	r := newRuntimeValidationTestRuntime(t)
	ctx := receiptCaller("receipt-owner", "chat-receipt")
	other := receiptCaller("receipt-other", "chat-other")
	sibling := receiptCaller("receipt-owner", "chat-sibling")
	marker := filepath.Join(t.TempDir(), "effects")
	command := fmt.Sprintf("printf x >> %q; printf x", marker)
	requestID := newExecutionRequestID(r)
	args := map[string]any{"cmd": command, "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": requestID, "env": map[string]any{"RECEIPT_SECRET": "super-secret-token"}}

	first, err := r.Call(ctx, "exec_command", args)
	if err != nil {
		t.Fatal(err)
	}
	if first["new_execution_started"] != true || first["stdout"] != "x" || effectCount(t, marker) != 1 {
		t.Fatalf("first = %#v", first)
	}
	receipt, _ := first["receipt"].(map[string]any)
	if receipt["call_id"] != first["call_id"] || receipt["execution_request_id"] != requestID || receipt["deduplication_scope"] != "runtime_epoch" {
		t.Fatalf("receipt = %#v", receipt)
	}
	replay, err := r.Call(ctx, "exec_command", args)
	if err != nil {
		t.Fatal(err)
	}
	if replay["new_execution_started"] != false || replay["call_id"] != first["call_id"] || replay["stdout"] != "x" || effectCount(t, marker) != 1 {
		t.Fatalf("replay = %#v", replay)
	}
	peeked, err := r.Call(ctx, "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID})
	if err != nil {
		t.Fatal(err)
	}
	peekReceipt, _ := peeked["receipt"].(map[string]any)
	if peeked["call_id"] == first["call_id"] || peekReceipt["call_id"] != first["call_id"] || peeked["stdout"] != "x" {
		t.Fatalf("peek = %#v", peeked)
	}

	conflictArgs := map[string]any{"cmd": command + " ", "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": requestID, "env": map[string]any{"RECEIPT_SECRET": "super-secret-token"}}
	_, err = r.Call(ctx, "exec_command", conflictArgs)
	if code := receiptToolCode(err); code != "EXECUTION_REQUEST_CONFLICT" || strings.Contains(fmt.Sprint(err), "super-secret-token") {
		t.Fatalf("conflict = %v", err)
	}
	if effectCount(t, marker) != 1 {
		t.Fatal("conflict started another effect")
	}

	swapped, err := json.Marshal(map[string]any{"execution_request_id": requestID, "timeout_ms": 2000, "execution_mode": "sync", "cmd": command, "env": map[string]any{"RECEIPT_SECRET": "super-secret-token"}})
	if err != nil {
		t.Fatal(err)
	}
	var reordered map[string]any
	if err = json.Unmarshal(swapped, &reordered); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Call(ctx, "exec_command", reordered); err != nil || effectCount(t, marker) != 1 {
		t.Fatalf("reordered replay error=%v count=%d", err, effectCount(t, marker))
	}

	secondID := newExecutionRequestID(r)
	if _, err = r.Call(ctx, "exec_command", map[string]any{"cmd": command, "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": secondID}); err != nil || effectCount(t, marker) != 2 {
		t.Fatalf("second id count=%d err=%v", effectCount(t, marker), err)
	}

	_, err = r.Call(other, "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID})
	if code := receiptToolCode(err); code != "EXECUTION_REQUEST_NOT_FOUND" || strings.Contains(err.Error(), "kept") || strings.Contains(fmt.Sprint(err), stringArg(first, "session_id")) {
		t.Fatalf("other owner = %v", err)
	}
	_, err = r.Call(sibling, "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID})
	if code := receiptToolCode(err); code != "SESSION_CONVERSATION_MISMATCH" {
		t.Fatalf("sibling conversation = %v", err)
	}

	_, err = r.Call(ctx, "exec_command", map[string]any{"cmd": "true", "execution_request_id": "zzzz", "execution_mode": "sync"})
	if code := receiptToolCode(err); code != "INVALID_ARGUMENT" {
		t.Fatalf("bad id = %v", err)
	}
	_, err = r.Call(ctx, "exec_command", map[string]any{"cmd": "true", "retry_of_call_id": "call_" + strings.Repeat("0", 32), "execution_mode": "sync"})
	if code := receiptToolCode(err); code != "INVALID_RETRY" {
		t.Fatalf("unknown retry = %v", err)
	}
}

func TestExecReceiptConcurrentClaimStartsOnce(t *testing.T) {
	r := newRuntimeValidationTestRuntime(t)
	ctx := receiptCaller("receipt-race", "chat-race")
	marker := filepath.Join(t.TempDir(), "effects")
	requestID := newExecutionRequestID(r)
	args := map[string]any{"cmd": fmt.Sprintf("printf x >> %q; printf x", marker), "execution_mode": "sync", "timeout_ms": 5000, "execution_request_id": requestID}
	const racers = 32
	var ready sync.WaitGroup
	var start sync.WaitGroup
	ready.Add(racers)
	start.Add(1)
	results := make([]Result, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ready.Done()
			start.Wait()
			results[index], errs[index] = r.Call(ctx, "exec_command", args)
		}(i)
	}
	ready.Wait()
	start.Done()
	wg.Wait()
	var callID string
	started := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if results[i]["call_id"] == "" {
			t.Fatalf("racer %d missing call id: %#v", i, results[i])
		}
		if callID == "" {
			callID = stringArg(results[i], "call_id")
		}
		if results[i]["call_id"] != callID {
			t.Fatalf("call ids diverged: %v vs %v", results[i]["call_id"], callID)
		}
		if results[i]["new_execution_started"] == true {
			started++
		}
	}
	if started != 1 || effectCount(t, marker) != 1 {
		t.Fatalf("starts=%d effects=%d", started, effectCount(t, marker))
	}
	r.executionMu.Lock()
	pending := len(r.pendingCalls)
	r.executionMu.Unlock()
	if pending != 0 {
		t.Fatalf("pending approvals = %d", pending)
	}
}

func TestExecReceiptPendingRejectAndScopeStayOnTheOriginalClaim(t *testing.T) {
	r := executionTestRuntime(t)
	ctx := receiptCaller("receipt-approval", "chat-approval")
	task, err := r.Call(ctx, "task_manage", map[string]any{"action": "create", "title": "Receipt", "goal": "one claim", "completion_conditions": []string{"claimed"}, "steps": []map[string]any{{"id": "verify", "title": "Verify"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Call(ctx, "task_manage", map[string]any{"action": "set_current", "task_id": stringArg(task, "task_id")}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "effects")
	requestID := newExecutionRequestID(r)
	args := map[string]any{"cmd": fmt.Sprintf("printf x >> %q; printf x", marker), "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": requestID}
	pending, err := r.Call(ctx, "exec_command", args)
	if err != nil {
		t.Fatal(err)
	}
	if pending["status"] != "pending_approval" || pending["new_execution_started"] != false || pending["executed"] != false || effectCount(t, marker) != 0 {
		t.Fatalf("pending = %#v", pending)
	}
	again, err := r.Call(ctx, "exec_command", args)
	if err != nil {
		t.Fatal(err)
	}
	if again["approval_id"] != pending["approval_id"] || again["call_id"] != pending["call_id"] || again["new_execution_started"] != false {
		t.Fatalf("duplicate pending = %#v", again)
	}
	r.executionMu.Lock()
	pendingCount := len(r.pendingCalls)
	r.executionMu.Unlock()
	if pendingCount != 1 {
		t.Fatalf("pending approvals = %d", pendingCount)
	}
	otherTask, err := r.Call(ctx, "task_manage", map[string]any{"action": "create", "title": "Other", "goal": "not this claim", "completion_conditions": []string{"other"}, "steps": []map[string]any{{"id": "other", "title": "Other"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Call(ctx, "task_manage", map[string]any{"action": "set_current", "task_id": stringArg(otherTask, "task_id")}); err != nil {
		t.Fatal(err)
	}
	scoped, err := r.Call(ctx, "exec_command", args)
	if err != nil {
		t.Fatal(err)
	}
	if scoped["task_id"] != task["task_id"] || scoped["call_id"] != pending["call_id"] || effectCount(t, marker) != 0 {
		t.Fatalf("scope changed under the same id: %#v", scoped)
	}
	if _, err = r.RuntimeApprovalDecision(ctx, stringArg(pending, "approval_id"), "reject", false); err != nil {
		t.Fatal(err)
	}
	rejected, err := r.Call(ctx, "exec_command", args)
	if err != nil {
		t.Fatal(err)
	}
	if rejected["new_execution_started"] != false || effectCount(t, marker) != 0 {
		t.Fatalf("rejected id started work: %#v", rejected)
	}
}

func TestExecReceiptLimitPruneHistoryAndEpochDoNotReplay(t *testing.T) {
	r := newRuntimeValidationTestRuntime(t)
	ctx := receiptCaller("receipt-limit", "chat-limit")
	marker := filepath.Join(t.TempDir(), "effects")
	command := fmt.Sprintf("printf x >> %q; printf x", marker)
	r.setReceiptLimits(1, 1)
	firstID := newExecutionRequestID(r)
	first, err := r.Call(ctx, "exec_command", map[string]any{"cmd": command, "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": firstID})
	if err != nil {
		t.Fatal(err)
	}
	secondID := newExecutionRequestID(r)
	_, err = r.Call(ctx, "exec_command", map[string]any{"cmd": command, "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": secondID})
	if code := receiptToolCode(err); code != "EXECUTION_RECEIPT_LIMIT" || effectCount(t, marker) != 1 {
		t.Fatalf("limit = %v count=%d", err, effectCount(t, marker))
	}
	replay, err := r.Call(ctx, "exec_command", map[string]any{"cmd": command, "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": firstID})
	if err != nil || replay["stdout"] != "x" || effectCount(t, marker) != 1 {
		t.Fatalf("existing lookup after limit = %#v %v", replay, err)
	}

	if removed := r.command.PruneCompletedBefore(time.Now().Add(time.Hour)); removed == 0 {
		t.Fatal("prune removed nothing")
	}
	pruned, err := r.Call(ctx, "session_observe", map[string]any{"action": "peek", "execution_request_id": firstID})
	if err != nil {
		t.Fatal(err)
	}
	if pruned["output_unavailable"] != true || pruned["stdout"] != nil || pruned["command_ok"] != nil || effectCount(t, marker) != 1 {
		t.Fatalf("pruned peek = %#v", pruned)
	}
	callID := stringArg(first, "call_id")
	if err = r.activity.ManageCall(ctx, callID, activity.MetadataChange{Action: "trash"}); err != nil {
		t.Fatal(err)
	}
	if err = r.activity.ManageCall(ctx, callID, activity.MetadataChange{Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	forgotten, err := r.Call(ctx, "session_observe", map[string]any{"action": "peek", "execution_request_id": firstID})
	if err != nil {
		t.Fatal(err)
	}
	if forgotten["history_unavailable"] != true || forgotten["status"] != "unknown" || forgotten["command_ok"] != nil || forgotten["exit_code"] != nil {
		t.Fatalf("forgotten = %#v", forgotten)
	}
	if _, err = r.Call(ctx, "exec_command", map[string]any{"cmd": command, "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": firstID}); err != nil || effectCount(t, marker) != 1 {
		t.Fatalf("deleted history replayed count=%d err=%v", effectCount(t, marker), err)
	}

	fresh := newRuntimeValidationTestRuntime(t)
	freshMarker := filepath.Join(t.TempDir(), "fresh-effects")
	_, err = fresh.Call(ctx, "exec_command", map[string]any{"cmd": fmt.Sprintf("printf x >> %q; printf x", freshMarker), "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": firstID})
	if code := receiptToolCode(err); code != "EXECUTION_EPOCH_MISMATCH" || effectCount(t, freshMarker) != 0 {
		t.Fatalf("new epoch = %v", err)
	}
}

func TestExecReceiptObservationCanBeDeniedWithoutRedispatch(t *testing.T) {
	r := newRuntimeValidationTestRuntime(t)
	ctx := receiptCaller("receipt-gate", "chat-gate")
	marker := filepath.Join(t.TempDir(), "effects")
	requestID := newExecutionRequestID(r)
	args := map[string]any{"cmd": fmt.Sprintf("printf x >> %q; printf x", marker), "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": requestID}
	if _, err := r.Call(ctx, "exec_command", args); err != nil {
		t.Fatal(err)
	}
	current, err := r.permissions.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rules := []permission.Rule{{ID: "deny-observe", Tool: "session_observe", Effect: permission.Deny, Reason: "fixture observe deny"}}
	if _, err = r.permissions.Update(ctx, permission.Change{Scope: "global", ExpectedRevision: current.Revision, Rules: &rules}); err != nil {
		t.Fatal(err)
	}
	_, err = r.Call(ctx, "exec_command", args)
	if code := receiptToolCode(err); code != "PERMISSION_DENIED" || effectCount(t, marker) != 1 {
		t.Fatalf("denied replay = %v count=%d", err, effectCount(t, marker))
	}
	restored, err := r.permissions.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	empty := []permission.Rule{}
	if _, err = r.permissions.Update(ctx, permission.Change{Scope: "global", Mode: permission.Full, ConfirmFull: true, ExpectedRevision: restored.Revision, Rules: &empty}); err != nil {
		t.Fatal(err)
	}
	restoredRead, err := r.Call(ctx, "session_observe", map[string]any{"action": "peek", "execution_request_id": requestID})
	if err != nil || restoredRead["stdout"] != "x" || effectCount(t, marker) != 1 {
		t.Fatalf("restored peek = %#v %v", restoredRead, err)
	}
}

func TestExecReceiptSessionLimitKeepsClaimWithoutReplay(t *testing.T) {
	r := newRuntimeValidationTestRuntime(t)
	ctx := receiptCaller("receipt-capacity", "chat-capacity")
	for i := 0; i < 32; i++ {
		started, err := r.Call(ctx, "exec_command", map[string]any{"cmd": "sleep 30", "execution_mode": "async", "timeout_ms": 60000})
		if err != nil || started["status"] != "running" {
			t.Fatalf("filler %d: %v %#v", i, err, started)
		}
	}
	marker := filepath.Join(t.TempDir(), "effects")
	requestID := newExecutionRequestID(r)
	args := map[string]any{"cmd": fmt.Sprintf("printf x >> %q", marker), "execution_mode": "sync", "timeout_ms": 2000, "execution_request_id": requestID}
	_, err := r.Call(ctx, "exec_command", args)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "SESSION_LIMIT_REACHED" || effectCount(t, marker) != 0 || toolErr.Details["execution_request_id"] != requestID {
		t.Fatalf("limit start = %#v", err)
	}
	replay, err := r.Call(ctx, "exec_command", args)
	if err != nil || replay["new_execution_started"] != false || replay["output_unavailable"] != true || replay["command_ok"] != nil || effectCount(t, marker) != 0 {
		t.Fatalf("limited replay = %#v %v", replay, err)
	}
}

func TestExecReceiptCloseDoesNotDeadlock(t *testing.T) {
	r := newRuntimeValidationTestRuntime(t)
	ctx := receiptCaller("receipt-close", "chat-close")
	requestID := newExecutionRequestID(r)
	if _, err := r.Call(ctx, "exec_command", map[string]any{"cmd": "sleep 30", "execution_mode": "async", "timeout_ms": 60000, "execution_request_id": requestID}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("runtime close did not return while a receipt session was running")
	}
}

func TestAgentDockContextPublishesCommandRecovery(t *testing.T) {
	r := newRuntimeValidationTestRuntime(t)
	result, err := r.Call(context.Background(), "agentdock_context", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := result["runtime"].(map[string]any)
	recovery, _ := runtime["command_recovery"].(map[string]any)
	if runtime["execution_epoch"] != r.executionEpoch() || recovery["request_id_field"] != "execution_request_id" || recovery["peek"] != true || recovery["durable"] != false || recovery["deduplication_scope"] != "runtime_epoch" {
		t.Fatalf("runtime recovery = %#v", runtime)
	}
	rules := fmt.Sprint(result["rules"])
	if !strings.Contains(rules, "execution_request_id") || strings.Contains(rules, "20") {
		t.Fatalf("bootstrap rules = %s", rules)
	}
}
