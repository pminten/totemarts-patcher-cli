package patcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// An XDelta instance provides helpers for invoking the xdelta program.
type XDelta struct {
	// Path to the binary.
	binPath string
}

// Create an XDelta instance.
//
// If the binPath is just a basename without directory it will be looked up in PATH.
// To use binary in the current directory use something like './xdelta3'.
func NewXDelta(binPath string) (*XDelta, error) {
	if dir, _ := filepath.Split(binPath); dir == "" {
		realPath, err := exec.LookPath(binPath)
		if err != nil {
			return nil, fmt.Errorf("failed to find '%s' in PATH: %w", binPath, err)
		}
		return &XDelta{binPath: realPath}, nil
	}
	if _, err := os.Stat(binPath); err != nil {
		return nil, fmt.Errorf("failed to find '%s': %w", binPath, err)
	}
	return &XDelta{
		binPath: binPath,
	}, nil
}

// ApplyPatch runs the xdelta binary, outputting to newPath, validating the checksum at the same time.
// If oldPath is not nil it's a delta patch, otherwise it's a full patch.
func (x XDelta) ApplyPatch(
	ctx context.Context,
	oldPath *string,
	patchPath string,
	newPath string,
	expectedChecksum string,
) error {
	// Validating the checksum here makes the xdelta code messier but saves a lot of time because
	// we don't have to read the file later.
	var cmd *exec.Cmd
	var what string
	if oldPath == nil {
		// Decompress, source window <num>, force overwrite, write to stdout.
		// Source window number is copied from Vue/Electron launcher.
		cmd = exec.CommandContext(ctx, x.binPath, "-d", "-B", "536870912", "-f", "-c", patchPath)
		what = fmt.Sprintf("applying full patch '%s' to get '%s'", patchPath, newPath)
	} else {
		// Decompress, source window <num>, force overwrite, write to stdout, source file to copy from (oldPath).
		cmd = exec.CommandContext(ctx, x.binPath, "-d", "-B", "536870912", "-f", "-c", "-s", *oldPath, patchPath)
		what = "delta patch"
		what = fmt.Sprintf("applying delta patch '%s' to '%s' to get '%s'", patchPath, *oldPath, newPath)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s failed (create stdout pipe): %w", what, err)
	}

	hash := sha256.New()
	wrappedStdout := io.TeeReader(stdout, hash)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s failed (start xdelta): %w", what, err)
	}

	file, err := os.Create(newPath)
	if err != nil {
		return fmt.Errorf("%s failed (create file): %w", what, err)
	}
	defer file.Close()

	_, err = io.Copy(file, wrappedStdout)
	if err != nil {
		return fmt.Errorf("%s failed (write file): %w", what, err)
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s failed: %w; xdelta said: %s",
				what, err, string(exitErr.Stderr))
		}
		return fmt.Errorf("%s failed: %w", what, err)
	}

	checksum := hex.EncodeToString(hash.Sum(nil))
	if !HashEqual(checksum, expectedChecksum) {
		return fmt.Errorf("%s failed: expected it to produce file with checksum %s but got %s",
			what, strings.ToUpper(expectedChecksum), strings.ToUpper(checksum))
	}

	return nil
}
