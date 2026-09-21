package command

import (
	"github.com/uvwt/agentdock/internal/tool/command/session"
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

const (
	ToolExecCommand    = "exec_command"
	ToolSessionObserve = "session_observe"
	ToolSessionAct     = "session_act"
)

func InputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	boolProp := toolcontract.Boolean
	boundedIntProp := toolcontract.BoundedInteger
	props := map[string]any{}
	var required []string

	switch name {
	case ToolExecCommand:
		toolcontract.ActivityProperties(props)
		toolcontract.TargetProperties(props)
		props["cmd"] = stringProp("Command to run.")
		props["workdir"] = stringProp(WorkdirDescription())
		AddRuntimeProperties(props)
		props["skill"] = stringProp("Optional active Skill context. When workdir is omitted, the command runs from the active installed Skill root and loads that Skill isolated environment.")
		props["skill_env"] = stringProp("Optional Skill name whose isolated environment is loaded without changing workdir. Kept for environment-only compatibility.")
		props["env"] = map[string]any{"type": "object", "description": "Explicit command environment values. These override the selected Skill environment.", "additionalProperties": map[string]any{"type": "string"}}
		props["timeout_ms"] = boundedIntProp("Timeout in milliseconds. Must be positive and is capped at 86400000.", 1, 86400000)
		props["execution_mode"] = map[string]any{"type": "string", "description": "Execution mode. Defaults to auto: wait up to yield_time_ms, then return a running session. sync waits for exit; async returns a session immediately.", "enum": []string{"auto", "sync", "async"}}
		props["yield_time_ms"] = boundedIntProp("Foreground wait threshold for execution_mode=auto. Defaults to 5000 and is capped at 30000 milliseconds.", 0, 30000)
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, MaxOutputBytes)
		props["stdin"] = stringProp("Initial stdin.")
		props["tty"] = boolProp("Keep stdin open.")
		required = []string{"cmd"}
	case ToolSessionObserve:
		props["action"] = map[string]any{"type": "string", "description": "Read-only session action. peek reads absolute output offsets without consuming the session.", "enum": []string{"list", "status", "peek"}}
		props["session_id"] = stringProp("Session id returned by exec_command. Required for status. For peek, supply exactly one of session_id or execution_request_id.")
		props["execution_request_id"] = stringProp("Execution request id. For peek, supply this or session_id, not both.")
		props["stdout_offset"] = outputOffsetProp("Absolute stdout byte offset for action=peek. Defaults to 0.")
		props["stderr_offset"] = outputOffsetProp("Absolute stderr byte offset for action=peek. Defaults to 0.")
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, MaxOutputBytes)
	case ToolSessionAct:
		props["action"] = map[string]any{"type": "string", "description": "Mutating session action.", "enum": []string{"write", "kill", "kill_all"}}
		props["session_id"] = stringProp("Session id returned by exec_command, required for write/kill.")
		props["chars"] = stringProp("Characters to write when action=write.")
		props["max_output_bytes"] = boundedIntProp("Maximum output bytes. Defaults to 65536 and is capped at 4194304.", 1, MaxOutputBytes)
	default:
		return nil, false
	}
	schema := toolcontract.InputObject(props, required...)
	if name == ToolSessionObserve {
		schema["allOf"] = sessionObserveActionConstraints()
	}
	return schema, true
}

func outputOffsetProp(description string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
		"minimum":     0,
		"maximum":     session.MaxSafeOutputOffset,
	}
}

func sessionObserveActionConstraints() []map[string]any {
	peekAction := map[string]any{
		"properties": map[string]any{"action": map[string]any{"const": "peek"}},
		"required":   []string{"action"},
	}
	legacyAction := map[string]any{
		"anyOf": []map[string]any{
			{"properties": map[string]any{"action": map[string]any{"enum": []string{"list", "status"}}}, "required": []string{"action"}},
			{"not": map[string]any{"required": []string{"action"}}},
		},
	}
	peekOnlyForbidden := map[string]any{
		"not": map[string]any{"anyOf": []map[string]any{
			{"required": []string{"stdout_offset"}},
			{"required": []string{"stderr_offset"}},
			{"required": []string{"execution_request_id"}},
		}},
	}
	return []map[string]any{
		{
			"if": peekAction,
			"then": map[string]any{
				"properties": map[string]any{
					"max_output_bytes": map[string]any{"minimum": minPeekOutputBytes},
				},
				"anyOf": []map[string]any{
					{"required": []string{"session_id"}, "not": map[string]any{"required": []string{"execution_request_id"}}},
					{"required": []string{"execution_request_id"}, "not": map[string]any{"required": []string{"session_id"}}},
				},
			},
		},
		{"if": legacyAction, "then": peekOnlyForbidden},
		{
			"if": map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": "status"}},
				"required":   []string{"action"},
			},
			"then": map[string]any{"required": []string{"session_id"}},
		},
	}
}

func OutputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	arrayProp := toolcontract.ObjectArray
	props := map[string]any{
		"task_id":              stringProp("Persisted task binding inherited by subsequent session operations."),
		"thread_id":            stringProp("Execution thread fixed when the command was started."),
		"step_id":              stringProp("Step associated with the original command."),
		"workspace_id":         stringProp("Resolved workspace for the original command."),
		"activity_warning":     stringProp("Command execution state is valid but the activity journal is incomplete."),
		"sessions":             arrayProp("Command session summaries returned by list or bulk session actions."),
		"count":                intProp("Command session count when a list or bulk action returns multiple sessions."),
		"session_id":           stringProp("Command session id."),
		"status":               stringProp("Session status."),
		"runtime":              stringProp("Command runtime when reported by the host, such as windows or wsl."),
		"wsl_distribution":     stringProp("WSL distribution selected for the command when explicitly configured."),
		"workdir":              stringProp("Logical command working directory in the selected runtime."),
		"stdout":               stringProp("Captured stdout segment."),
		"stderr":               stringProp("Captured stderr segment."),
		"stdout_next_offset":   intProp("Absolute stdout offset immediately after this peek."),
		"stderr_next_offset":   intProp("Absolute stderr offset immediately after this peek."),
		"stdout_retained_from": intProp("Absolute stdout offset of the oldest byte still retained."),
		"stderr_retained_from": intProp("Absolute stderr offset of the oldest byte still retained."),
		"stdout_total_bytes":   intProp("Total stdout bytes produced by the process."),
		"stderr_total_bytes":   intProp("Total stderr bytes produced by the process."),
		"stdout_gap":           boolProp("True when the requested stdout offset fell before the retained range."),
		"stderr_gap":           boolProp("True when the requested stderr offset fell before the retained range."),
		"stdout_truncated":     boolProp("True when this response stopped before the retained stdout end because of max_output_bytes."),
		"stderr_truncated":     boolProp("True when this response stopped before the retained stderr end because of max_output_bytes."),
		"stdout_pending_utf8":  boolProp("True when a running stdout stream ended on an incomplete UTF-8 sequence that was left unread."),
		"stderr_pending_utf8":  boolProp("True when a running stderr stream ended on an incomplete UTF-8 sequence that was left unread."),
		"stdout_invalid_utf8":  boolProp("True when stdout display text replaced invalid bytes."),
		"stderr_invalid_utf8":  boolProp("True when stderr display text replaced invalid bytes."),
		"command_ok":           boolProp("Whether a completed command exited successfully. Omitted while the command is still running."),
		"command_error":        stringProp("Command process error when execution did not succeed."),
		"exit_code":            intProp("Process exit code, when available."),
		"elapsed_ms":           intProp("Session elapsed milliseconds."),
		"timed_out":            boolProp("Whether the command timed out."),
	}
	switch name {
	case ToolExecCommand:
		props["session_reason"] = stringProp("Why exec_command returned a session instead of a completed result.")
		props["observe_after_ms"] = intProp("Suggested delay before inspecting the returned session.")
	case ToolSessionObserve, ToolSessionAct:
	default:
		return nil, false
	}
	return toolcontract.OutputObject(props), true
}
