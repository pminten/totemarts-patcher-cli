package patcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// An XDelta instance provides helpers for invoking the xdelta program.
type XDelta struct {
	// Path to the binary.
	binPath string
}

// Create an XDelta instance.
//
// If the binPath is just a basename without directory will look in PATH and also in the current directory.
func NewXDelta(binPath string) (*XDelta, error) {
	if dir, _ := filepath.Split(binPath); dir == "" {
		realPath, err := exec.LookPath(binPath)
		if err != nil && !errors.Is(err, exec.ErrDot) {
			return nil, fmt.Errorf("failed to find %q in PATH: %w", binPath, err)
		}
		return &XDelta{
			binPath: realPath,
		}, nil
	}
	if _, err := os.Stat(binPath); err != nil {
		return nil, fmt.Errorf("failed to find %q: %w", binPath, err)
	}
	return &XDelta{
		binPath: binPath,
	}, nil
}

// ApplyDeltaPath applies a delta patch to upgrade the file at oldPath to newPath.
func (x XDelta) ApplyDeltaPatch(ctx context.Context, oldPath string, patchPath string, newPath string) error {
	// Decompress, source window <num>, force overwrite, source file to copy from (oldPath).
	// Source window number is copied from Vue/Electron launcher.
	cmd := exec.CommandContext(ctx, x.binPath, "-d", "-B", "536870912", "-f", "-s", oldPath, patchPath, newPath)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("applying delta patch %q to %q to get %q failed: %w; xdelta said: %s",
				oldPath, patchPath, newPath, err, string(exitErr.Stderr))
		}
		return fmt.Errorf("applying delta patch %q to %q to get %q failed: %w",
			oldPath, patchPath, newPath, err)
	}
	return nil
}

// ApplyFullPath applies a full / replacement patch to create the file at newPath.
func (x XDelta) ApplyFullPatch(ctx context.Context, patchPath string, newPath string) error {
	// Decompress, source window <num>, force overwrite.
	cmd := exec.CommandContext(ctx, x.binPath, "-d", "-B", "536870912", "-f", patchPath, newPath)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("applying full patch %q to get %q failed: %w; xdelta said: %s",
				patchPath, newPath, err, string(exitErr.Stderr))
		}
		return fmt.Errorf("applying full patch %q to get %q failed: %w",
			patchPath, newPath, err)
	}
	return nil
}
