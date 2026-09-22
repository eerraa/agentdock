package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestBundledRipgrepSupportIsWindowsAMD64Only(t *testing.T) {
	if !bundledRipgrepSupported("windows", "amd64") {
		t.Fatal("windows/amd64 must be the only bundled ripgrep target")
	}
	for _, platform := range [][2]string{
		{"windows", "arm64"},
		{"windows", "386"},
		{"linux", "amd64"},
		{"darwin", "arm64"},
	} {
		if bundledRipgrepSupported(platform[0], platform[1]) {
			t.Fatalf("%s/%s must keep the existing PATH and Go search path", platform[0], platform[1])
		}
	}
}

func TestRipgrepPinMatchesManifest(t *testing.T) {
	var manifest struct {
		Name             string            `json:"name"`
		Version          string            `json:"version"`
		Target           string            `json:"target"`
		ArchiveURL       string            `json:"archive_url"`
		ArchiveSHA256    string            `json:"archive_sha256"`
		ArchiveRoot      string            `json:"archive_root"`
		Executable       string            `json:"executable"`
		ExecutableSHA256 string            `json:"executable_sha256"`
		ExecutableBytes  int64             `json:"executable_bytes"`
		License          string            `json:"license"`
		PayloadDir       string            `json:"payload_dir"`
		LicenseFiles     map[string]string `json:"license_files"`
	}
	data, err := os.ReadFile(filepath.Join(agentdockModuleRoot(t), "packaging", "windows", "third_party", "ripgrep", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "ripgrep" || manifest.Version != "15.2.0" || manifest.Target != "x86_64-pc-windows-msvc" {
		t.Fatalf("unexpected ripgrep identity: %+v", manifest)
	}
	if manifest.Executable != "rg.exe" || manifest.ExecutableSHA256 != bundledRipgrepDigestPin {
		t.Fatalf("executable pin = %s %s", manifest.Executable, manifest.ExecutableSHA256)
	}
	if manifest.ExecutableBytes != 4218880 || manifest.License != "Unlicense OR MIT" || manifest.PayloadDir != bundledRipgrepPayloadDir {
		t.Fatalf("unexpected ripgrep payload metadata: %+v", manifest)
	}
	wantURL := "https://github.com/BurntSushi/ripgrep/releases/download/15.2.0/ripgrep-15.2.0-x86_64-pc-windows-msvc.zip"
	if manifest.ArchiveURL != wantURL || manifest.ArchiveRoot != "ripgrep-15.2.0-x86_64-pc-windows-msvc" {
		t.Fatalf("archive identity = %s %s", manifest.ArchiveURL, manifest.ArchiveRoot)
	}
	if len(manifest.ArchiveSHA256) != 64 || strings.ToLower(manifest.ArchiveSHA256) != manifest.ArchiveSHA256 {
		t.Fatalf("archive sha256 = %s", manifest.ArchiveSHA256)
	}
	wantLicenses := map[string]struct{}{"COPYING": {}, "LICENSE-MIT": {}, "UNLICENSE": {}}
	if len(manifest.LicenseFiles) != len(wantLicenses) {
		t.Fatalf("license files = %#v", manifest.LicenseFiles)
	}
	for name, hash := range manifest.LicenseFiles {
		if _, ok := wantLicenses[name]; !ok || len(hash) != 64 || strings.ToLower(hash) != hash {
			t.Fatalf("license pin %s = %s", name, hash)
		}
	}
}

func TestRipgrepPayloadPathIsShared(t *testing.T) {
	root := agentdockModuleRoot(t)
	for _, check := range []struct{ path, needle string }{
		{"packaging/windows/third_party/ripgrep/manifest.json", `"payload_dir": "third_party/ripgrep"`},
		{"packaging/windows/fetch-ripgrep.ps1", "third_party/ripgrep"},
		{"packaging/windows/build-windows-release.ps1", "fetch-ripgrep.ps1"},
		{"packaging/windows/build-windows-offline-setup.ps1", "-AssertReleaseArchive"},
		{"scripts/test/verify-windows-release-assets.ps1", "-AssertReleaseArchive"},
		{"internal/installer/apply.go", `filepath.Join(payload, "third_party", "ripgrep")`},
		{"internal/selfupdate/generation_windows.go", `filepath.Join(desktopStaged, "third_party", "ripgrep")`},
		{"internal/selfupdate/desktop_update_windows.go", `"third_party/ripgrep/rg.exe"`},
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(check.path)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), check.needle) {
			t.Fatalf("%s does not keep the ripgrep payload path %q", check.path, check.needle)
		}
	}
}

func TestEvaluateBundledRipgrep(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("official-bytes")
	path := filepath.Join(dir, "rg.exe")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"COPYING", "LICENSE-MIT", "UNLICENSE", "NOTICE"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(payload)
	previous := bundledRipgrepDigestOverride
	bundledRipgrepDigestOverride = hex.EncodeToString(sum[:])
	t.Cleanup(func() { bundledRipgrepDigestOverride = previous })

	if got, status := evaluateBundledRipgrep(path); status != bundleVerified || got != path {
		t.Fatalf("verified bundle = %q %v", got, status)
	}
	if err := os.WriteFile(filepath.Join(dir, "evil.dll"), []byte("dll"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, status := evaluateBundledRipgrep(path); status != bundleRejected || got != path {
		t.Fatalf("directory with an extra file = %q %v", got, status)
	}
	if err := os.Remove(filepath.Join(dir, "evil.dll")); err != nil {
		t.Fatal(err)
	}
	bundledRipgrepDigestOverride = strings.Repeat("ab", 32)
	if _, status := evaluateBundledRipgrep(path); status != bundleRejected {
		t.Fatalf("hash mismatch status = %v", status)
	}
	if got, status := evaluateBundledRipgrep(filepath.Join(t.TempDir(), "rg.exe")); status != bundleAbsent || got != "" {
		t.Fatalf("missing bundle = %q %v", got, status)
	}
}

func TestSearchTextPrefersInjectedBundleOverPath(t *testing.T) {
	bundle := fakeRipgrep(t, "from-bundle")
	pathRG := fakeRipgrep(t, "from-path")
	consultedPath := false
	withSearchRipgrep(t, func() (string, bundleStatus) {
		return bundle, bundleVerified
	}, func() (string, error) {
		consultedPath = true
		return pathRG, nil
	})

	result := searchNeedle(t)
	if consultedPath {
		t.Fatal("PATH rg was consulted after the bundled rg verified")
	}
	assertSearchMatch(t, result, "rg", "from-bundle")
}

func TestSearchTextUsesPathWhenBundleMissing(t *testing.T) {
	pathRG := fakeRipgrep(t, "from-path")
	withSearchRipgrep(t, func() (string, bundleStatus) {
		return "", bundleAbsent
	}, func() (string, error) {
		return pathRG, nil
	})
	assertSearchMatch(t, searchNeedle(t), "rg", "from-path")
}

func TestSearchTextSkipsRejectedBundleEvenIfPathResolvesToIt(t *testing.T) {
	bundle := fakeRipgrep(t, "from-bundle")
	withSearchRipgrep(t, func() (string, bundleStatus) {
		return bundle, bundleRejected
	}, func() (string, error) {
		return bundle, nil
	})
	assertSearchMatch(t, searchNeedle(t), "go_fallback", "needle")
}

func TestSearchTextUsesPathWhenBundleIsRejectedButDistinct(t *testing.T) {
	bundle := fakeRipgrep(t, "from-bundle")
	pathRG := fakeRipgrep(t, "from-path")
	withSearchRipgrep(t, func() (string, bundleStatus) {
		return bundle, bundleRejected
	}, func() (string, error) {
		return pathRG, nil
	})
	assertSearchMatch(t, searchNeedle(t), "rg", "from-path")
}

func TestSearchTextUsesGoFallbackWhenNoRipgrep(t *testing.T) {
	withSearchRipgrep(t, func() (string, bundleStatus) {
		return "", bundleAbsent
	}, func() (string, error) {
		return "", exec.ErrNotFound
	})
	assertSearchMatch(t, searchNeedle(t), "go_fallback", "needle")
}

func withSearchRipgrep(t *testing.T, inspect func() (string, bundleStatus), lookPath func() (string, error)) {
	t.Helper()
	previousInspect := inspectBundledRipgrep
	previousLookPath := searchRipgrepLookPath
	if inspect != nil {
		inspectBundledRipgrep = inspect
	}
	if lookPath != nil {
		searchRipgrepLookPath = lookPath
	}
	t.Cleanup(func() {
		inspectBundledRipgrep = previousInspect
		searchRipgrepLookPath = previousLookPath
	})
}

func searchNeedle(t *testing.T) Result {
	t.Helper()
	rt, root := newCodeToolsRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.SearchText(context.Background(), SearchRequest{
		Path: ".", Query: "needle", CaseSensitive: true, MaxResults: intPtrForTest(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertSearchMatch(t *testing.T, result Result, engine, matchText string) {
	t.Helper()
	if result["engine"] != engine {
		t.Fatalf("engine = %#v, want %s; result = %#v", result["engine"], engine, result)
	}
	matches, ok := result["matches"].([]map[string]any)
	if !ok || len(matches) != 1 || matches[0]["match_text"] != matchText {
		t.Fatalf("matches = %#v, want one %s match", result["matches"], matchText)
	}
}

func agentdockModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

var fakeRipgrepCache sync.Map

const fakeRipgrepSource = `package main

import (
	"encoding/json"
	"os"
)

func main() {
	payload := map[string]any{
		"type": "match",
		"data": map[string]any{
			"path":  map[string]any{"text": "sample.txt"},
			"lines": map[string]any{"text": "needle\n"},
			"line_number": 1,
			"submatches": []any{
				map[string]any{"match": map[string]any{"text": %q}, "start": 0},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		os.Exit(2)
	}
	if _, err := os.Stdout.Write(append(data, '\n')); err != nil {
		os.Exit(2)
	}
}
`

func fakeRipgrep(t *testing.T, token string) string {
	t.Helper()
	if strings.ContainsAny(token, "\\\"\r\n") {
		t.Fatalf("fake ripgrep token must stay a plain literal: %q", token)
	}
	if cached, ok := fakeRipgrepCache.Load(token); ok {
		return cached.(string)
	}
	dir, err := os.MkdirTemp("", "agentdock-fake-rg-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake-rg\n\ngo 1.26.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(fmt.Sprintf(fakeRipgrepSource, token)), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "rg")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on", "CGO_ENABLED=0", "GOFLAGS=", "GOTOOLCHAIN=local")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake ripgrep: %v\n%s", err, output)
	}
	fakeRipgrepCache.Store(token, binary)
	return binary
}
