package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestCopyWindowsGenerationPayloadKeepsRipgrepOptional(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	writeGenerationPayload(t, payload)
	staging := filepath.Join(root, "staging")
	if err := copyWindowsGenerationPayload(payload, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(staging, updateengine.GenerationCoreName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(staging, "third_party", "ripgrep", "rg.exe")); !os.IsNotExist(err) {
		t.Fatalf("missing ripgrep payload was published: %v", err)
	}

	writeRipgrepPayload(t, filepath.Join(payload, "third_party", "ripgrep"))
	withRipgrep := filepath.Join(root, "with-ripgrep")
	if err := copyWindowsGenerationPayload(payload, withRipgrep); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"rg.exe":      "rg",
		"COPYING":     "copying",
		"LICENSE-MIT": "mit",
		"UNLICENSE":   "unlicense",
		"NOTICE":      "notice",
	} {
		data, err := os.ReadFile(filepath.Join(withRipgrep, "third_party", "ripgrep", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", name, data, want)
		}
	}
}

func TestCopyWindowsGenerationPayloadRejectsUnexpectedRipgrepFile(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	writeGenerationPayload(t, payload)
	ripgrep := filepath.Join(payload, "third_party", "ripgrep")
	writeRipgrepPayload(t, ripgrep)
	if err := os.WriteFile(filepath.Join(ripgrep, "evil.dll"), []byte("dll"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyWindowsGenerationPayload(payload, filepath.Join(root, "staging")); err == nil {
		t.Fatal("expected an unexpected ripgrep payload file to stop publication")
	}
}

func writeGenerationPayload(t *testing.T, payload string) {
	t.Helper()
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agentdock.exe", "agentdock-tray.exe", "agentdock-arbiter.exe"} {
		if err := os.WriteFile(filepath.Join(payload, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeRipgrepPayload(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"rg.exe":      "rg",
		"COPYING":     "copying",
		"LICENSE-MIT": "mit",
		"UNLICENSE":   "unlicense",
		"NOTICE":      "notice",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
