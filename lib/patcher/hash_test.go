package patcher

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashReaderAndHashBytes(t *testing.T) {
	// Very simple comparison smoke test, these things are very straightforward.
	data := make([]byte, 10000)
	_, err := io.ReadAtLeast(rand.Reader, data, len(data))
	require.NoError(t, err)
	actual, err := HashReader(bytes.NewReader(data))
	require.NoError(t, err)
	expected := HashBytes(data)
	require.EqualValues(t, expected, actual)
}
