package command

import "strings"

func (s *Service) Observe(request SessionObserveRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		if err := rejectPeekOnlyFields(request); err != nil {
			return nil, err
		}
		return s.listSessions()
	case "status":
		if err := rejectPeekOnlyFields(request); err != nil {
			return nil, err
		}
		return s.sessionStatus(request)
	case "peek":
		return s.peekSession(request)
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_observe action", "validation", map[string]any{"action": request.Action, "allowed": []string{"list", "status", "peek"}})
	}
}

func rejectPeekOnlyFields(request SessionObserveRequest) error {
	if !peekFieldsPresent(request) {
		return nil
	}
	return toolError("INVALID_ARGUMENT", "stdout_offset, stderr_offset, and execution_request_id are only valid for action=peek", "validation")
}

func (s *Service) Act(request SessionActRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "write":
		return s.writeStdin(request)
	case "kill":
		return s.killSession(request)
	case "kill_all":
		return s.killAll()
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported session_act action", "validation", map[string]any{"action": request.Action, "allowed": []string{"write", "kill", "kill_all"}})
	}
}
