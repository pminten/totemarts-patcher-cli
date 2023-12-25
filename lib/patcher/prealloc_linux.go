//go:build !windows && linux

package patcher

import (
	"fmt"
	"os"
	"syscall"
)

// CreateWithSizeHint creates a file, reserving space to grow it to size.
func CreateWithSizeHint(filename string, size int64) (file *os.File, err error) {
	file, err = os.Create(filename)
	if err != nil {
		err = fmt.Errorf("creating file failed: %w", err)
		return
	}
	defer func() {
		// Only act if there's an error as we need to return that handle.
		if err != nil {
			file.Close()
		}
	}()
	if err = syscall.Fallocate(int(file.Fd()), 0, 0, size); err != nil {
		err = fmt.Errorf("fallocating size %d failed: %w", size, err)
		return
	}
	return
}
