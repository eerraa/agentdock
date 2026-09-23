//go:build race

package activity

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The complete 100k-event workload and every functional assertion still run
// under -race. Wall-clock production budgets are additionally checked by an
// uncached, non-race execution of this exact fixture and toolchain. No test or
// budget is skipped, and native failure/missing toolchain is a hard failure.
// See https://go.dev/doc/articles/race_detector#runtime-overhead.
func assertExecutionScaleBudget(t *testing.T, cold, update time.Duration) {
	t.Helper()
	t.Logf("race-instrumented timings: cold=%s incremental=%s; validating unchanged production budgets separately", cold, update)
	goName := "go"
	if runtime.GOOS == "windows" {
		goName += ".exe"
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", goName)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goTool, "test", "-mod=readonly", "-race=false", "-short=false", "-run", "^TestExecutionProjectionScale100k$", "-count=1", "-timeout=90s", "-v", ".")
	// An inherited GOFLAGS=-race or -tags=race must not recurse into this
	// helper. Other test environment values, including cache paths, survive.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "GOFLAGS") && !strings.EqualFold(key, "GOTOOLCHAIN") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GOFLAGS=", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("same-source non-race scale budget failed using %s: %v\n%s", goTool, err, output)
	}
	if !bytes.Contains(output, []byte("scale_budget_mode=non-race; cold_limit=2s; incremental_limit=1s")) ||
		!bytes.Contains(output, []byte("--- PASS: TestExecutionProjectionScale100k")) || bytes.Contains(output, []byte("--- SKIP:")) {
		t.Fatalf("non-race scale budget did not execute and pass: %s", output)
	}
	t.Logf("same-source non-race budget proof (%s):\n%s", runtime.Version(), output)
}
