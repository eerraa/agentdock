package file

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// bundledRipgrepDigestPin is the SHA-256 of the official unmodified
	// ripgrep 15.2.0 Windows amd64 rg.exe. packaging/windows/third_party/ripgrep/manifest.json
	// is the same pin; TestRipgrepPinMatchesManifest keeps them together.
	bundledRipgrepDigestPin  = "14231169855ec5205cf5a1b6f1db358ff4aed4247c86b69ce8aae647c77f6680"
	bundledRipgrepPayloadDir = "third_party/ripgrep"
	maxBundledRipgrepBytes   = 32 << 20
)

// bundledRipgrepDigestOverride is empty in production. Tests set it so a
// temporary executable can satisfy the same check as the official pin.
var bundledRipgrepDigestOverride string

var searchRipgrepLookPath = func() (string, error) {
	return exec.LookPath("rg")
}

var inspectBundledRipgrep = defaultInspectBundledRipgrep

type bundleStatus int

const (
	bundleAbsent bundleStatus = iota
	bundleRejected
	bundleVerified
)

func bundledRipgrepSupported(goos, goarch string) bool {
	return goos == "windows" && goarch == "amd64"
}

func currentBundledRipgrepDigest() string {
	if bundledRipgrepDigestOverride != "" {
		return bundledRipgrepDigestOverride
	}
	return bundledRipgrepDigestPin
}

// resolveSearchRipgrep 的顺序是：校验通过的 Windows amd64 官方 rg.exe、
// 宿主 PATH 里的 rg，再由调用方回退到 Go。缺失、哈希不符或目录里有多余文件时不执行该路径。
// WSL 搜索在进入这里之前已经返回，不会执行这份 Windows rg.exe。
func resolveSearchRipgrep() (string, bool) {
	bundled, status := inspectBundledRipgrep()
	if status == bundleVerified && strings.TrimSpace(bundled) != "" {
		return bundled, true
	}
	path, err := searchRipgrepLookPath()
	if err != nil || strings.TrimSpace(path) == "" {
		return "", false
	}
	if status == bundleRejected && sameSearchExecutable(path, bundled) {
		return "", false
	}
	return path, true
}

func evaluateBundledRipgrep(path string) (string, bundleStatus) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", bundleAbsent
		}
		return path, bundleRejected
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return path, bundleRejected
	}
	if !bundledRipgrepDirectoryTrusted(filepath.Dir(path)) || !ripgrepDigestMatches(path) {
		return path, bundleRejected
	}
	return path, bundleVerified
}

func bundledRipgrepDirectoryTrusted(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	allowed := map[string]struct{}{
		"rg.exe":      {},
		"COPYING":     {},
		"LICENSE-MIT": {},
		"UNLICENSE":   {},
		"NOTICE":      {},
	}
	foundExecutable := false
	for _, entry := range entries {
		if _, ok := allowed[entry.Name()]; !ok {
			return false
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return false
		}
		if entry.Name() == "rg.exe" {
			foundExecutable = true
		}
	}
	return foundExecutable
}

func ripgrepDigestMatches(path string) bool {
	expected, err := hex.DecodeString(currentBundledRipgrepDigest())
	if err != nil || len(expected) != sha256.Size {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(file, maxBundledRipgrepBytes+1))
	if err != nil || n <= 0 || n > maxBundledRipgrepBytes {
		return false
	}
	return subtle.ConstantTimeCompare(sum.Sum(nil), expected) == 1
}

func sameSearchExecutable(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo) {
		return true
	}
	leftAbs, leftAbsErr := filepath.Abs(left)
	rightAbs, rightAbsErr := filepath.Abs(right)
	if leftAbsErr != nil || rightAbsErr != nil {
		return false
	}
	leftClean := filepath.Clean(leftAbs)
	rightClean := filepath.Clean(rightAbs)
	if filepath.Separator == '\\' {
		return strings.EqualFold(leftClean, rightClean)
	}
	return leftClean == rightClean
}
