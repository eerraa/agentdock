package activity

import (
	"fmt"
	"testing"
	"time"
)

// Production budgets are unchanged. Instrumented runs also execute the same
// fixture without instrumentation instead of relaxing either limit.
func executionScaleBudgetErrors(cold, update time.Duration) []error {
	var failures []error
	if cold > 2*time.Second {
		failures = append(failures, fmt.Errorf("cold execution query exceeded the planned 2s local target: %s", cold))
	}
	if update > time.Second {
		failures = append(failures, fmt.Errorf("incremental projection exceeded the planned 1s local target: %s", update))
	}
	return failures
}

func TestExecutionScaleBudgetBoundaries(t *testing.T) {
	for _, test := range []struct {
		name         string
		cold, update time.Duration
		failures     int
	}{
		{"below", time.Second, time.Millisecond, 0},
		{"exact", 2 * time.Second, time.Second, 0},
		{"cold-over", 2*time.Second + time.Nanosecond, time.Second, 1},
		{"update-over", 2 * time.Second, time.Second + time.Nanosecond, 1},
		{"both-over", 2*time.Second + time.Nanosecond, time.Second + time.Nanosecond, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if failures := executionScaleBudgetErrors(test.cold, test.update); len(failures) != test.failures {
				t.Fatalf("budget boundaries changed: got %v want %d failures", failures, test.failures)
			}
		})
	}
}
