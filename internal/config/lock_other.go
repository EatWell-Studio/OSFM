//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package config

import "os"

// lockFile does nothing on platforms without flock (OSFM is released for Linux and macOS only): there, nothing stops
// `osfm restore` from running while the instance does.
func lockFile(*os.File, bool) error { return nil }
