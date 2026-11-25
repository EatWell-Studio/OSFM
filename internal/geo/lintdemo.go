package geo

import "os"

// lintDemo exists only to show that CI rejects lint errors; it is reverted in the next commit.
func lintDemo() {
	os.Remove("osfm-lint-demo")
}

var _ = lintDemo
