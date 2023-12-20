package patcher

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var manDate1 time.Time = time.Date(2023, 12, 15, 14, 46, 23, 325, time.UTC)
var manDate2 time.Time = time.Date(2023, 12, 15, 14, 46, 23, 324, time.UTC)

func TestManifestSmoke(t *testing.T) {
	// Quick and dirty test of the manifest functions.
	tempDir := t.TempDir()
	man1 := NewManifest("foo")
	man1.Add(filepath.Join("a", "b"), manDate1, "abcde")
	require.True(t, man1.Check(filepath.Join("a", "b"), manDate1, "abcde"))
	require.NoError(t, man1.WriteManifest(tempDir))
	man2, err := ReadManifest(tempDir, "foo")
	require.NoError(t, err)
	require.True(t, man2.Check(filepath.Join("a", "b"), manDate1, "abcde"))
	require.False(t, man2.Check(filepath.Join("a", "c"), manDate1, "abcde"))
	require.False(t, man2.Check(filepath.Join("a", "b"), manDate2, "abcde"))
	require.False(t, man2.Check(filepath.Join("a", "b"), manDate1, "abcdef"))
	h1, found := man2.Get(filepath.Join("a", "b"), manDate1)
	require.True(t, found)
	require.Equal(t, "abcde", h1)
	_, found = man2.Get(filepath.Join("a", "b"), manDate2)
	require.False(t, found)
}

func TestReadManifestInvalidProduct(t *testing.T) {
	tempDir := t.TempDir()
	man1 := NewManifest("foo")
	man1.Add(filepath.Join("a", "b"), manDate1, "abcde")
	require.True(t, man1.Check(filepath.Join("a", "b"), manDate1, "abcde"))
	require.NoError(t, man1.WriteManifest(tempDir))
	_, err := ReadManifest(tempDir, "bar")
	require.ErrorContains(t, err, "wrong product")
}

func TestReadManifestNotFound(t *testing.T) {
	tempDir := t.TempDir()
	man, err := ReadManifest(tempDir, "foo")
	require.NoError(t, err)
	require.Equal(t, "foo", man.Product)
	require.Empty(t, man.Entries)
}
