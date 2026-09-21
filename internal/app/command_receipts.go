package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/activity"
	"github.com/uvwt/agentdock/internal/permission"
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
)

const (
	defaultReceiptLimit      = 16384
	defaultReceiptOwnerLimit = 4096
	executionReceiptPrefix   = "agentdock.exec.receipt.v1\n"
)

var executionRequestIDPattern = regexp.MustCompile(`^[a-f0-9]{32}\.[a-f0-9]{32}$`)
var errReceiptLimit = errors.New("execution receipt limit")

// commandReceipt is the in-memory claim for one owner and execution_request_id.
// It stores the digest and identifiers, not the command body, environment, or output.
type commandReceipt struct {
	ownerKey     string
	requestID    string
	digest       [32]byte
	callID       string
	initial      activity.Binding
	confirmed    activity.Binding
	hasConfirmed bool
	sessionID    string
	approvalID   string
	claimedAt    time.Time
	prepared     bool
	failed       bool
}

type receiptIndex struct {
	byOwner     map[string]map[string]*commandReceipt
	byCall      map[string]*commandReceipt
	total       int
	maxTotal    int
	maxPerOwner int
}

func newReceiptIndex(total, perOwner int) *receiptIndex {
	if total <= 0 {
		total = defaultReceiptLimit
	}
	if perOwner <= 0 {
		perOwner = defaultReceiptOwnerLimit
	}
	return &receiptIndex{
		byOwner:     map[string]map[string]*commandReceipt{},
		byCall:      map[string]*commandReceipt{},
		maxTotal:    total,
		maxPerOwner: perOwner,
	}
}

func (r *Runtime) setReceiptLimits(total, perOwner int) {
	r.executionMu.Lock()
	defer r.executionMu.Unlock()
	if r.receipts == nil {
		r.receipts = newReceiptIndex(total, perOwner)
		return
	}
	r.receipts.maxTotal = total
	r.receipts.maxPerOwner = perOwner
}

func (r *Runtime) executionEpoch() string {
	epoch := strings.TrimPrefix(r.executionInstance, "call_")
	if len(epoch) != 32 || epoch == r.executionInstance {
		return ""
	}
	return epoch
}

func (r *Runtime) callExecReceipt(ctx context.Context, spec ToolSpec, original map[string]any) (Result, error) {
	if original == nil {
		original = map[string]any{}
	}
	snapshot, resolveErr := r.resolveExecutionScope(ctx)
	if resolveErr != nil {
		if errors.Is(resolveErr, activity.ErrConversationDeleted) {
			return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("CONVERSATION_TERMINATED", activity.ConversationTerminatedMessage, "permission"))
		}
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("CONVERSATION_BINDING_ERROR", resolveErr.Error(), "validation"))
	}
	if err := r.checkConversationGate(ctx, snapshot.ConversationID); err != nil {
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, err)
	}
	if err := r.validateToolArguments(spec.Name, original); err != nil {
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, err)
	}
	args, err := cloneExecutionArguments(original)
	if err != nil {
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("INVALID_ARGUMENT", "tool arguments must be JSON-compatible", "validation"))
	}
	requestID := stringArg(args, "execution_request_id")
	if !executionRequestIDPattern.MatchString(requestID) {
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("INVALID_ARGUMENT", "execution_request_id must be a 32-hex runtime epoch, a dot, and a 32-hex nonce", "validation"))
	}
	if requestID[:32] != r.executionEpoch() {
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("EXECUTION_EPOCH_MISMATCH", "execution_request_id belongs to a different runtime epoch. Check the original effect before starting another execution.", "conflict"))
	}
	digest, err := executionReceiptDigest(args)
	if err != nil {
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("INVALID_ARGUMENT", "execution arguments cannot be canonicalized", "validation"))
	}
	preflight := snapshot
	preflight.TaskID, preflight.ThreadID, preflight.StepID, preflight.WorkspaceID = "", "", "", ""
	claimed, existed, err := r.claimExecReceipt(activity.SourceOwnerKey(ctx), requestID, digest, preflight)
	if errors.Is(err, errReceiptLimit) {
		return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("EXECUTION_RECEIPT_LIMIT", "execution receipt metadata is at capacity. Existing ids can still be read. Do not drop the id or restart the runtime to force a new execution.", "resource_limit"))
	}
	if err != nil {
		return nil, err
	}
	if existed {
		if claimed.digest != digest {
			return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolErrorDetails("EXECUTION_REQUEST_CONFLICT", "execution_request_id was already claimed with different arguments", "conflict", map[string]any{"execution_request_id": requestID}))
		}
		fresh, ok := r.receiptSnapshot(activity.SourceOwnerKey(ctx), requestID)
		if !ok {
			return r.auditUnclaimedRejection(ctx, spec, original, snapshot, toolError("EXECUTION_REQUEST_NOT_FOUND", "execution request was not found for this runtime and owner", "not_found"))
		}
		result, readErr := r.readExecReceipt(ctx, snapshot, fresh, outputLimitArg(args))
		if readErr != nil {
			return r.auditUnclaimedRejection(ctx, spec, original, snapshot, readErr)
		}
		return result, nil
	}
	return r.dispatchObservedCall(ctx, spec, original, claimed.callID)
}

func (r *Runtime) peekExecReceipt(ctx context.Context, request toolcommand.SessionObserveRequest) (Result, error) {
	requestID := strings.TrimSpace(request.ExecutionRequestID)
	if !executionRequestIDPattern.MatchString(requestID) {
		return nil, toolError("INVALID_ARGUMENT", "execution_request_id must be a 32-hex runtime epoch, a dot, and a 32-hex nonce", "validation")
	}
	if requestID[:32] != r.executionEpoch() {
		return nil, toolError("EXECUTION_EPOCH_MISMATCH", "execution_request_id belongs to a different runtime epoch. Check the original effect before starting another execution.", "conflict")
	}
	snap, ok := r.receiptSnapshot(activity.SourceOwnerKey(ctx), requestID)
	if !ok {
		return nil, toolError("EXECUTION_REQUEST_NOT_FOUND", "execution request was not found for this runtime and owner", "not_found")
	}
	return r.readExecReceipt(ctx, activity.FromContext(ctx), snap, request.MaxOutputBytes)
}

func (r *Runtime) readExecReceipt(ctx context.Context, current activity.Binding, snap commandReceipt, limit *int) (Result, error) {
	original := snap.initial
	if snap.hasConfirmed {
		original = snap.confirmed
	}
	if err := r.authorizeReceiptRead(ctx, current, original); err != nil {
		return nil, err
	}
	if found, ok := r.command.SessionForCall(snap.callID); ok && found != nil {
		result, err := r.command.Observe(toolcommand.SessionObserveRequest{Action: "peek", SessionID: found.ID, MaxOutputBytes: limit})
		if err != nil {
			return nil, err
		}
		r.noteReceiptSession(snap.callID, found.ID)
		snap.sessionID = found.ID
		r.finishReceiptRead(result, snap)
		applyReceiptBinding(result, original)
		result["call_id"] = snap.callID
		return result, nil
	}
	if snap.approvalID != "" && !snap.failed {
		call, err := r.activity.Call(ctx, snap.callID)
		if err == nil && (call.Status == "pending_approval" || call.Status == "created") {
			result := Result{"status": "pending_approval", "executed": false, "approval_id": snap.approvalID}
			r.finishReceiptRead(result, snap)
			applyReceiptBinding(result, original)
			result["call_id"] = snap.callID
			return result, nil
		}
	}
	if !snap.prepared && !snap.failed && snap.approvalID == "" {
		result := Result{"status": "preparing", "executed": false}
		r.finishReceiptRead(result, snap)
		applyReceiptBinding(result, original)
		result["call_id"] = snap.callID
		return result, nil
	}
	call, err := r.activity.Call(ctx, snap.callID)
	if err != nil {
		result := Result{"status": "unknown", "history_unavailable": true}
		r.finishReceiptRead(result, snap)
		result["call_id"] = snap.callID
		result["execution_request_id"] = snap.requestID
		return result, nil
	}
	status := call.Status
	if status == "" {
		status = "unknown"
	}
	result := Result{"status": status, "output_unavailable": true}
	if call.ApprovalID != "" && snap.approvalID == "" {
		snap.approvalID = call.ApprovalID
	}
	r.finishReceiptRead(result, snap)
	applyReceiptBinding(result, original)
	result["call_id"] = snap.callID
	return result, nil
}

func (r *Runtime) authorizeReceiptRead(ctx context.Context, current, original activity.Binding) error {
	if err := r.checkConversationGate(ctx, current.ConversationID); err != nil {
		return err
	}
	if err := r.authorizeSessionBinding(ctx, original, current); err != nil {
		return err
	}
	decision, err := r.permissions.Decide(ctx, permission.Facts{Binding: current, Tool: "session_observe", Action: "peek", ReadOnly: true})
	if err != nil {
		return err
	}
	if decision.Effect != permission.Allow {
		reason := decision.Reason
		if reason == "" {
			reason = "session observation is not allowed"
		}
		return toolErrorDetails("PERMISSION_DENIED", reason, "permission", map[string]any{"rule_id": decision.RuleID, "executed": false})
	}
	return nil
}

func (r *Runtime) finishReceiptRead(result Result, snap commandReceipt) {
	if result == nil {
		return
	}
	result["execution_request_id"] = snap.requestID
	result["execution_epoch"] = r.executionEpoch()
	result["new_execution_started"] = false
	result["receipt"] = receiptPayload(snap)
}

func (r *Runtime) claimExecReceipt(owner, requestID string, digest [32]byte, initial activity.Binding) (commandReceipt, bool, error) {
	callID, err := activity.NewExecutionID("call_")
	if err != nil {
		return commandReceipt{}, false, err
	}
	r.executionMu.Lock()
	defer r.executionMu.Unlock()
	if r.receipts == nil {
		r.receipts = newReceiptIndex(defaultReceiptLimit, defaultReceiptOwnerLimit)
	}
	if existing := r.receipts.byOwner[owner][requestID]; existing != nil {
		return *existing, true, nil
	}
	if r.receipts.total >= r.receipts.maxTotal || len(r.receipts.byOwner[owner]) >= r.receipts.maxPerOwner {
		return commandReceipt{}, false, errReceiptLimit
	}
	item := &commandReceipt{
		ownerKey: owner, requestID: requestID, digest: digest, callID: callID,
		initial: initial, claimedAt: time.Now(),
	}
	if r.receipts.byOwner[owner] == nil {
		r.receipts.byOwner[owner] = map[string]*commandReceipt{}
	}
	r.receipts.byOwner[owner][requestID] = item
	r.receipts.byCall[callID] = item
	r.receipts.total++
	return *item, false, nil
}

func (r *Runtime) receiptSnapshot(owner, requestID string) (commandReceipt, bool) {
	r.executionMu.Lock()
	defer r.executionMu.Unlock()
	if r.receipts == nil {
		return commandReceipt{}, false
	}
	item := r.receipts.byOwner[owner][requestID]
	if item == nil {
		return commandReceipt{}, false
	}
	return *item, true
}

func (r *Runtime) receiptByCall(callID string) (commandReceipt, bool) {
	r.executionMu.Lock()
	defer r.executionMu.Unlock()
	if r.receipts == nil || callID == "" {
		return commandReceipt{}, false
	}
	item := r.receipts.byCall[callID]
	if item == nil {
		return commandReceipt{}, false
	}
	return *item, true
}

func (r *Runtime) markReceiptPrepared(callID string, binding activity.Binding) {
	r.updateReceipt(callID, func(item *commandReceipt) {
		item.confirmed = binding
		item.hasConfirmed = true
		item.prepared = true
	})
}

func (r *Runtime) markReceiptFailed(callID string) {
	r.updateReceipt(callID, func(item *commandReceipt) {
		item.failed = true
		item.prepared = true
	})
}

func (r *Runtime) noteReceiptApproval(callID, approvalID string) {
	r.updateReceipt(callID, func(item *commandReceipt) {
		item.approvalID = approvalID
		item.prepared = true
	})
}

func (r *Runtime) noteReceiptSession(callID, sessionID string) {
	r.updateReceipt(callID, func(item *commandReceipt) {
		if sessionID != "" {
			item.sessionID = sessionID
		}
		item.prepared = true
	})
}

func (r *Runtime) updateReceipt(callID string, update func(*commandReceipt)) {
	if callID == "" || update == nil {
		return
	}
	r.executionMu.Lock()
	defer r.executionMu.Unlock()
	if r.receipts == nil {
		return
	}
	item := r.receipts.byCall[callID]
	if item == nil {
		return
	}
	update(item)
}

func (r *Runtime) annotateReceiptResult(result Result, callID string, newExecution bool) {
	item, ok := r.receiptByCall(callID)
	if !ok || result == nil {
		return
	}
	result["execution_request_id"] = item.requestID
	result["execution_epoch"] = r.executionEpoch()
	result["new_execution_started"] = newExecution
	result["receipt"] = receiptPayload(item)
}

func (r *Runtime) auditUnclaimedRejection(ctx context.Context, spec ToolSpec, original map[string]any, binding activity.Binding, failure error) (Result, error) {
	callID, err := activity.NewExecutionID("call_")
	if err != nil {
		return nil, err
	}
	binding.CallID = callID
	binding.ParentCallID = activity.FromContext(ctx).CallID
	state := executionObservation{binding: binding, started: time.Now()}
	if err = r.appendExecution(activity.Event{Binding: binding, Kind: "call.created", Status: "created", ToolName: spec.Name, Title: spec.Title}); err != nil {
		return nil, toolError("AUDIT_UNAVAILABLE", "The execution journal is unavailable; the tool was not dispatched.", "runtime")
	}
	event := activity.Event{Binding: binding, Kind: "call.completed", Status: "failed", ToolName: spec.Name, Title: spec.Title, ElapsedMS: time.Since(state.started).Milliseconds(), Summary: r.executionRedactor(original).Text(failure.Error(), 4096)}
	var toolErr *ToolError
	if errors.As(failure, &toolErr) {
		event.ErrorCode = toolErr.Code
	}
	_ = r.appendExecution(event)
	return nil, r.executionError(failure, state)
}

func executionReceiptDigest(args map[string]any) ([32]byte, error) {
	cloned, err := cloneExecutionArguments(args)
	if err != nil {
		return [32]byte{}, err
	}
	delete(cloned, "execution_request_id")
	encoded, err := json.Marshal(cloned)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte(executionReceiptPrefix), encoded...)), nil
}

func receiptPayload(item commandReceipt) map[string]any {
	payload := map[string]any{
		"version":              1,
		"execution_request_id": item.requestID,
		"call_id":              item.callID,
		"deduplication_scope":  "runtime_epoch",
	}
	if item.sessionID != "" {
		payload["session_id"] = item.sessionID
	}
	if item.approvalID != "" {
		payload["approval_id"] = item.approvalID
	}
	return payload
}

func applyReceiptBinding(result Result, binding activity.Binding) {
	if result == nil {
		return
	}
	if binding.ConversationID != "" {
		result["conversation_id"] = binding.ConversationID
	}
	if binding.CallID != "" {
		result["call_id"] = binding.CallID
	}
	if binding.TaskID != "" {
		result["task_id"] = binding.TaskID
	}
	if binding.ThreadID != "" {
		result["thread_id"] = binding.ThreadID
	}
	if binding.StepID != "" {
		result["step_id"] = binding.StepID
	}
	if binding.WorkspaceID != "" {
		result["workspace_id"] = binding.WorkspaceID
	}
}

func outputLimitArg(args map[string]any) *int {
	value, ok := args["max_output_bytes"]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case float64:
		number := int(typed)
		return &number
	case int:
		number := typed
		return &number
	case int64:
		number := int(typed)
		return &number
	default:
		return nil
	}
}
