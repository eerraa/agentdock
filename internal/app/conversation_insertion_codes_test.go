package app

import (
	"context"
	"errors"
	"github.com/uvwt/agentdock/internal/activity"
	"strings"
	"testing"
)

func TestInsertionStableCodesPreserveLocalOwnershipAndLimits(t *testing.T) {
	r := executionTestRuntime(t)
	result := scopeRead(t, r, scopeHost("localized-insertion-codes"))
	id := stringArg(result, "conversation_id")
	local := activity.WithLocalManagement(context.Background())
	if _, err := r.RuntimeEnqueueInsertion(local, id, InsertionRequest{SubmissionID: "req_localized", Text: "original"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		request InsertionRequest
		code    string
	}{
		{InsertionRequest{SubmissionID: "req_localized", Text: "changed"}, "INSERTION_CONFLICT"},
		{InsertionRequest{SubmissionID: "req_oversized", Text: strings.Repeat("a", 8193)}, "INSERTION_LIMIT"},
	} {
		_, err := r.RuntimeEnqueueInsertion(local, id, tc.request)
		var issue *ToolError
		if !errors.As(err, &issue) || issue.Code != tc.code {
			t.Fatalf("code=%v want %s", err, tc.code)
		}
	}
	if _, err := r.RuntimeEnqueueInsertion(context.Background(), id, InsertionRequest{SubmissionID: "req_remote", Text: "remote"}); !errors.Is(err, activity.ErrConversationOwner) {
		t.Fatalf("local-only guard changed: %v", err)
	}
}
