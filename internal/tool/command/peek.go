package command

import (
	"errors"
	"strings"

	"github.com/uvwt/agentdock/internal/tool/command/session"
)

const minPeekOutputBytes = 4

func (s *Service) peekSession(request SessionObserveRequest) (Result, error) {
	if err := validatePeekRequest(request); err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		// Request-id lookup is resolved by the runtime before this call.
		// A bare id that reaches the command service has no claim here.
		return nil, toolError("EXECUTION_REQUEST_NOT_FOUND", "execution request was not found for this runtime", "not_found")
	}
	found, ok := s.sessions.Get(sessionID)
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	maxBytes := intValue(request.MaxOutputBytes, 65536)
	read, err := found.ReadOutputAt(offsetValue(request.StdoutOffset), offsetValue(request.StderrOffset), maxBytes)
	if err != nil {
		return nil, outputCursorToolError(err)
	}
	return peekResult(read), nil
}

func validatePeekRequest(request SessionObserveRequest) error {
	sessionID := strings.TrimSpace(request.SessionID)
	requestID := strings.TrimSpace(request.ExecutionRequestID)
	if (sessionID == "") == (requestID == "") {
		return toolError("INVALID_ARGUMENT", "peek requires exactly one of session_id or execution_request_id", "validation")
	}
	if request.MaxOutputBytes != nil {
		value := *request.MaxOutputBytes
		if value < minPeekOutputBytes || value > MaxOutputBytes {
			return toolErrorDetails("INVALID_ARGUMENT", "peek max_output_bytes must be between 4 and 4194304", "validation", map[string]any{"max_output_bytes": value, "minimum": minPeekOutputBytes, "maximum": MaxOutputBytes})
		}
	}
	if err := validateOutputOffset(request.StdoutOffset, "stdout_offset"); err != nil {
		return err
	}
	return validateOutputOffset(request.StderrOffset, "stderr_offset")
}

func validateOutputOffset(value *int64, field string) error {
	if value == nil {
		return nil
	}
	if *value < 0 || *value > session.MaxSafeOutputOffset {
		return toolErrorDetails("INVALID_OUTPUT_CURSOR", "output offset is outside the representable byte range", "validation", map[string]any{"field": field, "offset": *value, "maximum": session.MaxSafeOutputOffset})
	}
	return nil
}

func offsetValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func outputCursorToolError(err error) error {
	var cursor *session.OutputCursorError
	if !errors.As(err, &cursor) || cursor == nil {
		return err
	}
	return toolErrorDetails("INVALID_OUTPUT_CURSOR", "output offset is outside the stream", "validation", map[string]any{
		"stream":      cursor.Stream,
		"offset":      cursor.Offset,
		"total_bytes": cursor.Total,
	})
}

func peekFieldsPresent(request SessionObserveRequest) bool {
	return request.StdoutOffset != nil || request.StderrOffset != nil || strings.TrimSpace(request.ExecutionRequestID) != ""
}

func peekResult(read session.OutputAt) Result {
	result := Result{
		"session_id":           read.SessionID,
		"status":               read.Status,
		"stdout":               read.Stdout.Text,
		"stderr":               read.Stderr.Text,
		"elapsed_ms":           read.ElapsedMS,
		"timed_out":            read.TimedOut,
		"terminal":             read.Terminal,
		"stdout_next_offset":   read.Stdout.NextOffset,
		"stderr_next_offset":   read.Stderr.NextOffset,
		"stdout_retained_from": read.Stdout.RetainedFrom,
		"stderr_retained_from": read.Stderr.RetainedFrom,
		"stdout_total_bytes":   read.Stdout.TotalBytes,
		"stderr_total_bytes":   read.Stderr.TotalBytes,
		"stdout_gap":           read.Stdout.Gap,
		"stderr_gap":           read.Stderr.Gap,
		"stdout_truncated":     read.Stdout.Truncated,
		"stderr_truncated":     read.Stderr.Truncated,
		"stdout_pending_utf8":  read.Stdout.PendingUTF8,
		"stderr_pending_utf8":  read.Stderr.PendingUTF8,
		"stdout_invalid_utf8":  read.Stdout.InvalidUTF8,
		"stderr_invalid_utf8":  read.Stderr.InvalidUTF8,
	}
	addBindingResult(result, read.Binding)
	if read.ActivityWarning != "" {
		result["activity_warning"] = read.ActivityWarning
	}
	if read.Completed {
		result["exit_code"] = read.ExitCode
		result["command_ok"] = read.CommandOK
	}
	if read.CommandError != "" {
		result["command_error"] = read.CommandError
	}
	if read.Runtime != "" {
		result["runtime"] = read.Runtime
	}
	if read.WSLDistribution != "" {
		result["wsl_distribution"] = read.WSLDistribution
	}
	if read.Workdir != "" {
		result["workdir"] = read.Workdir
	}
	return result
}
