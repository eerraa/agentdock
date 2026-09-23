//go:build windows

package desktopruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSetupResultReadWaitsForWindowsFileLocks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		byteRange bool
		wantError error
	}{
		{name: "sharing", wantError: windows.ERROR_SHARING_VIOLATION},
		{name: "byte-range", byteRange: true, wantError: windows.ERROR_LOCK_VIOLATION},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "result.json")
			want := setupLaunchResult{TaskName: "AgentDock Setup Native locked-current", PID: 123, ExitCode: 23}
			if err := writeSetupJSON(path, want); err != nil {
				t.Fatal(err)
			}
			name, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			var share uint32
			if tc.byteRange {
				share = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
			}
			handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
				share, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				t.Fatal(err)
			}
			closed := false
			defer func() {
				if !closed {
					_ = windows.CloseHandle(handle)
				}
			}()
			if tc.byteRange {
				if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
					0, 1, 0, &windows.Overlapped{}); err != nil {
					t.Fatal(err)
				}
			}
			_, originalReadError := os.ReadFile(path)
			if !errors.Is(originalReadError, tc.wantError) || errors.Is(originalReadError, os.ErrNotExist) {
				t.Fatalf("did not reproduce the original broker's fatal non-missing read: %v", originalReadError)
			}
			t.Logf("reproduced native read failure before handling: %v", originalReadError)
			result, ready, err := readSetupLaunchResult(path)
			if err != nil || ready || result.TaskName != "" {
				t.Fatalf("locked receipt acknowledged or aborted the launch: result=%+v ready=%v err=%v", result, ready, err)
			}
			if err := windows.CloseHandle(handle); err != nil {
				t.Fatal(err)
			}
			closed = true
			result, ready, err = readSetupLaunchResult(path)
			if err != nil || !ready || result != want {
				t.Fatalf("released receipt lost its nonce or child failure: result=%+v ready=%v err=%v", result, ready, err)
			}
		})
	}
}

func TestSetupResultReadDoesNotHideOtherErrors(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.json")
	if _, ready, err := readSetupLaunchResult(missing); ready || err != nil {
		t.Fatalf("missing result is not pending: ready=%v err=%v", ready, err)
	}
	if _, ready, err := readSetupLaunchResult(root); ready || err == nil {
		t.Fatalf("directory I/O error was hidden: ready=%v err=%v", ready, err)
	}
	malformed := filepath.Join(root, "invalid.json")
	if err := os.WriteFile(malformed, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := readSetupLaunchResult(malformed); ready || err == nil {
		t.Fatalf("malformed result was hidden: ready=%v err=%v", ready, err)
	}
}
