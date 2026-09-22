//go:build !windows

package desktopruntime

import "context"

func HoldCoreServer(context.Context, string) (func(), error) {
	return func() {}, nil
}
