//go:build !windows

package file

import "testing"

func TestDefaultInspectBundledRipgrepIsAbsent(t *testing.T) {
	got, status := defaultInspectBundledRipgrep()
	if status != bundleAbsent || got != "" {
		t.Fatalf("non-Windows bundle = %q %v", got, status)
	}
}
