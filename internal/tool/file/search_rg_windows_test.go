//go:build windows

package file

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSearchTextPrefersRipgrepBesideWindowsExecutable(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("bundled ripgrep is only selected on windows/amd64")
	}
	bundle := installBundledFakeRipgrep(t, "from-bundle")
	pathRG := fakeRipgrep(t, "from-path")
	consultedPath := false
	useBundledRipgrepExecutable(t, bundle.executable, hex.EncodeToString(bundle.digest[:]), func() (string, error) {
		consultedPath = true
		return pathRG, nil
	})

	result := searchNeedle(t)
	if consultedPath {
		t.Fatal("PATH rg was consulted after the bundled rg verified")
	}
	assertSearchMatch(t, result, "rg", "from-bundle")
}

func TestSearchTextSkipsDamagedRipgrepBesideWindowsExecutable(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("bundled ripgrep is only selected on windows/amd64")
	}
	bundle := installBundledFakeRipgrep(t, "from-bundle")
	if err := os.WriteFile(filepath.Join(bundle.directory, "evil.dll"), []byte("dll"), 0o600); err != nil {
		t.Fatal(err)
	}
	pathRG := fakeRipgrep(t, "from-path")
	useBundledRipgrepExecutable(t, bundle.executable, hex.EncodeToString(bundle.digest[:]), func() (string, error) {
		return pathRG, nil
	})
	assertSearchMatch(t, searchNeedle(t), "rg", "from-path")
}

func TestSearchTextSkipsDamagedRipgrepWhenPathResolvesToIt(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("bundled ripgrep is only selected on windows/amd64")
	}
	bundle := installBundledFakeRipgrep(t, "from-bundle")
	useBundledRipgrepExecutable(t, bundle.executable, bundledRipgrepDigestPin, func() (string, error) {
		return filepath.Join(bundle.directory, "rg.exe"), nil
	})
	assertSearchMatch(t, searchNeedle(t), "go_fallback", "needle")
}

func TestInspectBundledRipgrepIgnoresNonAMD64(t *testing.T) {
	bundle := installBundledFakeRipgrep(t, "from-bundle")
	previous := bundledRipgrepDigestOverride
	bundledRipgrepDigestOverride = hex.EncodeToString(bundle.digest[:])
	t.Cleanup(func() { bundledRipgrepDigestOverride = previous })
	executable := func() (string, error) { return bundle.executable, nil }

	if got, status := inspectBundledRipgrepAt("arm64", executable); status != bundleAbsent || got != "" {
		t.Fatalf("arm64 bundle = %q %v", got, status)
	}
	if runtime.GOARCH == "amd64" {
		got, status := inspectBundledRipgrepAt("amd64", executable)
		if status != bundleVerified || got != filepath.Join(bundle.directory, "rg.exe") {
			t.Fatalf("amd64 bundle = %q %v", got, status)
		}
	}
}

type bundledFakeRipgrep struct {
	executable string
	directory  string
	digest     [sha256.Size]byte
}

func installBundledFakeRipgrep(t *testing.T, token string) bundledFakeRipgrep {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "agentdock-core.exe")
	if err := os.WriteFile(executable, []byte("core"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "third_party", "ripgrep")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(fakeRipgrep(t, token))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "rg.exe"), payload, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"COPYING", "LICENSE-MIT", "UNLICENSE", "NOTICE"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return bundledFakeRipgrep{executable: executable, directory: directory, digest: sha256.Sum256(payload)}
}

func useBundledRipgrepExecutable(t *testing.T, executable, digest string, lookPath func() (string, error)) {
	t.Helper()
	previousDigest := bundledRipgrepDigestOverride
	previousExecutable := searchRipgrepExecutable
	previousLookPath := searchRipgrepLookPath
	bundledRipgrepDigestOverride = digest
	searchRipgrepExecutable = func() (string, error) { return executable, nil }
	searchRipgrepLookPath = lookPath
	t.Cleanup(func() {
		bundledRipgrepDigestOverride = previousDigest
		searchRipgrepExecutable = previousExecutable
		searchRipgrepLookPath = previousLookPath
	})
}
