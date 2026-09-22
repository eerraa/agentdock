//go:build windows

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsRipgrepArchiveFilesAreOptionalAndComplete(t *testing.T) {
	for _, name := range windowsRipgrepArchiveFiles {
		if _, ok := windowsGenerationArchiveFiles[name]; !ok {
			t.Fatalf("ripgrep archive file is not extracted: %s", name)
		}
	}
	files := map[string][]byte{
		"agentdock-tray.exe": []byte("tray"),
		"agentdock.ico":      []byte("icon"),
	}
	for _, name := range windowsRipgrepArchiveFiles {
		files[name] = []byte(filepath.Base(name))
	}
	root, err := extractDesktopUpdateArchive(context.Background(), makeWindowsDesktopArchive(t, files), t.TempDir(), "1.1.3")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range windowsRipgrepArchiveFiles {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != filepath.Base(name) {
			t.Fatalf("%s = %q", name, data)
		}
	}

	incomplete := map[string][]byte{
		"agentdock-tray.exe":         []byte("tray"),
		"agentdock.ico":              []byte("icon"),
		"third_party/ripgrep/rg.exe": []byte("rg"),
	}
	_, err = extractDesktopUpdateArchive(context.Background(), makeWindowsDesktopArchive(t, incomplete), t.TempDir(), "1.1.3")
	if err == nil || !strings.Contains(err.Error(), "ripgrep") {
		t.Fatalf("incomplete ripgrep archive error = %v", err)
	}
}

func TestCopyOptionalWindowsRipgrep(t *testing.T) {
	staging := t.TempDir()
	if err := copyOptionalWindowsRipgrep(t.TempDir(), staging); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(staging, "third_party")); !os.IsNotExist(err) {
		t.Fatalf("absent ripgrep payload was staged: %v", err)
	}

	source := t.TempDir()
	dir := filepath.Join(source, "third_party", "ripgrep")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rg.exe", "COPYING", "LICENSE-MIT", "UNLICENSE", "NOTICE"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(t.TempDir(), "generation")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyOptionalWindowsRipgrep(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "third_party", "ripgrep", "rg.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "rg.exe" {
		t.Fatalf("rg.exe = %q", data)
	}

	if err := os.WriteFile(filepath.Join(dir, "evil.dll"), []byte("dll"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyOptionalWindowsRipgrep(source, filepath.Join(t.TempDir(), "rejected")); err == nil || !strings.Contains(err.Error(), "evil.dll") {
		t.Fatalf("extra ripgrep file error = %v", err)
	}
}
