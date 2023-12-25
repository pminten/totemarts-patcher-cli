//go:build !windows && !linux

package patcher

import "os"

// CreateWithSizeHint opens a file for writing. This default implementation does not preallocate anything.
func CreateWithSizeHint(filename string, size int64) (file *os.File, err error) {
	return os.Create(filename)
}
