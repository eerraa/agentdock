//go:build !windows

package file

func defaultInspectBundledRipgrep() (string, bundleStatus) {
	return "", bundleAbsent
}
