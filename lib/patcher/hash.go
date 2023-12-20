package patcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
)

// HashBytes generates a SHA256 hash of a byte slice.
func HashBytes(data []byte) string {
	hash := sha256.New()
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

// HashReader reads data via a reader and computes a SHA256 hash of it.
func HashReader(ctx context.Context, s io.Reader) (string, error) {
	hash := sha256.New()
	// Reading up to 1 meg to try to avoid unnecessary syscalls. There's no guarantee that this
	// much data is returned of course, it just allows for it.
	buf := make([]byte, 1<<20)
	for ctx.Err() == nil {
		read, err := s.Read(buf)
		// Per docs for (io.Reader).Read the number of bytes read should be processed
		// before the error.
		if read > 0 {
			hash.Write(buf[:read])
		}
		if err != nil {
			if err == io.EOF {
				return hex.EncodeToString(hash.Sum(nil)), nil
			}
			return "", err
		}
	}
	return "", ctx.Err()
}

// HashEqual compares two hashes for equality.
func HashEqual(hash1 string, hash2 string) bool {
	return strings.EqualFold(hash1, hash2)
}
