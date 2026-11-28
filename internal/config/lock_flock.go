//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package config

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile takes a shared or exclusive flock on f without waiting.
func lockFile(f *os.File, exclusive bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	for {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB) // #nosec G115 -- a file descriptor fits in an int
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK):
			return ErrDataDirLocked
		default:
			return fmt.Errorf("lock %s: %w", f.Name(), err)
		}
	}
}
