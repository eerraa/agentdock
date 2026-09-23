//go:build !race

package activity

import (
	"testing"
	"time"
)

func assertExecutionScaleBudget(t *testing.T, cold, update time.Duration) {
	t.Helper()
	for _, failure := range executionScaleBudgetErrors(cold, update) {
		t.Error(failure)
	}
	if !t.Failed() {
		t.Log("scale_budget_mode=non-race; cold_limit=2s; incremental_limit=1s")
	}
}
