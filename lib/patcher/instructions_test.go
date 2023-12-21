package patcher

import (
	"crypto/sha256"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeInstructionsRealistic(t *testing.T) {
	// Check that a piece of real data deserializes properly.
	jsonData := []byte(`
	[{
		"Path":"Binaries\\InstallData\\dotNetFx40_Full_setup.exe",
		"OldHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"NewHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"CompressedHash":"1854E191B7DB2537CF1F27DBC512D0FED8C661329EC6BC8A0290BFB125CC12C0",
		"DeltaHash":null,
		"OldLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"NewLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"FullReplaceSize":794539,
		"DeltaSize":0,
		"HasDelta":false,
		"isComplete":false,
		"isActive":false
	}]
	`)
	expected := []Instruction{{
		Path:            filepath.Join("Binaries", "InstallData", "dotNetFx40_Full_setup.exe"),
		OldHash:         "FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		NewHash:         someStr("FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425"),
		CompressedHash:  someStr("1854E191B7DB2537CF1F27DBC512D0FED8C661329EC6BC8A0290BFB125CC12C0"),
		DeltaHash:       nil,
		FullReplaceSize: 794539,
		DeltaSize:       0,
	}}
	// Bypass the hash check.
	hash := sha256.New()
	hash.Write(jsonData)
	actual, err := DecodeInstructions(jsonData)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestDecodeInstructionsAbsolutePath(t *testing.T) {
	jsonData := []byte(`
	[{
		"Path":"C:\\Binaries\\InstallData\\dotNetFx40_Full_setup.exe",
		"OldHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"NewHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"CompressedHash":"1854E191B7DB2537CF1F27DBC512D0FED8C661329EC6BC8A0290BFB125CC12C0",
		"DeltaHash":null,
		"OldLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"NewLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"FullReplaceSize":794539,
		"DeltaSize":0,
		"HasDelta":false,
		"isComplete":false,
		"isActive":false
	}]
	`)
	// Bypass the hash check.
	hash := sha256.New()
	hash.Write(jsonData)
	_, err := DecodeInstructions(jsonData)
	require.ErrorContains(t, err, "absolute path")
}

func TestDecodeInstructionsNonLocalPath(t *testing.T) {
	jsonData := []byte(`
	[{
		"Path":"..\\Binaries\\InstallData\\dotNetFx40_Full_setup.exe",
		"OldHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"NewHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"CompressedHash":"1854E191B7DB2537CF1F27DBC512D0FED8C661329EC6BC8A0290BFB125CC12C0",
		"DeltaHash":null,
		"OldLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"NewLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"FullReplaceSize":794539,
		"DeltaSize":0,
		"HasDelta":false,
		"isComplete":false,
		"isActive":false
	}]
	`)
	// Bypass the hash check.
	hash := sha256.New()
	hash.Write(jsonData)
	_, err := DecodeInstructions(jsonData)
	require.ErrorContains(t, err, "non-local path")
}

func TestDecodeInstructionsInconsistentHasDeltaPath(t *testing.T) {
	jsonData := []byte(`
	[{
		"Path":"Binaries\\InstallData\\dotNetFx40_Full_setup.exe",
		"OldHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"NewHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"CompressedHash":"1854E191B7DB2537CF1F27DBC512D0FED8C661329EC6BC8A0290BFB125CC12C0",
		"DeltaHash":null,
		"OldLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"NewLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"FullReplaceSize":794539,
		"DeltaSize":0,
		"HasDelta":true,
		"isComplete":false,
		"isActive":false
	}]
	`)
	// Bypass the hash check.
	hash := sha256.New()
	hash.Write(jsonData)
	_, err := DecodeInstructions(jsonData)
	require.ErrorContains(t, err, "HasDelta set but no DeltaHash")
}

func TestDecodeInstructionsInconsistentNoHasDeltaPath(t *testing.T) {
	jsonData := []byte(`
	[{
		"Path":"Binaries\\InstallData\\dotNetFx40_Full_setup.exe",
		"OldHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"NewHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"CompressedHash":"1854E191B7DB2537CF1F27DBC512D0FED8C661329EC6BC8A0290BFB125CC12C0",
		"DeltaHash":"FA1AFFF978325F8818CE3A559D67A58297D9154674DE7FD8EB03656D93104425",
		"OldLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"NewLastWriteTime":"2022-12-03T06:35:03.6677356Z",
		"FullReplaceSize":794539,
		"DeltaSize":0,
		"HasDelta":false,
		"isComplete":false,
		"isActive":false
	}]
	`)
	// Bypass the hash check.
	hash := sha256.New()
	hash.Write(jsonData)
	_, err := DecodeInstructions(jsonData)
	require.ErrorContains(t, err, "HasDelta unset but contains a DeltaHash")
}
