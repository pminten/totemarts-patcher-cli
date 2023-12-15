package patcher

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManifestSmoke(t *testing.T) {
	// Quick and dirty test of the manifest functions.
	l := time.UTC
	tempDir := t.TempDir()
	man1 := NewManifest("foo")
	man1.Add(filepath.Join("a", "b"), time.Date(2023, 12, 15, 14, 46, 23, 325, l), "abcde")
	require.True(t, man1.Check(filepath.Join("a", "b"), time.Date(2023, 12, 15, 14, 46, 23, 325, l), "abcde"))
	require.NoError(t, man1.WriteManifest(tempDir))
	man2, err := ReadManifest(tempDir)
	require.NoError(t, err)
	require.True(t, man2.Check(filepath.Join("a", "b"), time.Date(2023, 12, 15, 14, 46, 23, 325, l), "abcde"))
	require.False(t, man2.Check(filepath.Join("a", "c"), time.Date(2023, 12, 15, 14, 46, 23, 325, l), "abcde"))
	require.False(t, man2.Check(filepath.Join("a", "b"), time.Date(2023, 12, 15, 14, 46, 23, 324, l), "abcde"))
	require.False(t, man2.Check(filepath.Join("a", "b"), time.Date(2023, 12, 15, 14, 46, 23, 325, l), "abcdef"))
}
