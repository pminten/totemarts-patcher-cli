package patcher

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// A rawInstruction contains the relevant part of an instruction from instructions.json
type rawInstruction struct {
	// Path relative to install dir. This is backslash encoded in the JSON.
	Path string `json:"Path"`
	// For a delta patch the hash that the existing file should have.
	OldHash string `json:"OldHash"`
	// The hash the file should have after patching. If nil the file should be deleted.
	NewHash *string `json:"NewHash"`
	// The hash of the full (not delta) patch file.
	CompressedHash string `json:"CompressedHash"`
	// If a delta patch exists the hash of the delta patch file.
	DeltaHash *string `json:"DeltaHash"`
	// Whether a delta patch exists.
	HasDelta bool `json:"HasDelta"`
	// Size in bytes of the full patch file.
	FullReplaceSize int64 `json:"FullReplaceSize"`
	// Size in bytes of the delta patch file. Zero if there is no delta patch file.
	DeltaSize int64 `json:"DeltaSize"`
}

// An Instruction contains the relevant part of an instruction from instructions.json
// with some minor processing.
type Instruction struct {
	// Path relative to install dir.  This is backslash encoded in the JSON but
	// DecodeInstructions will transform backslashes to slashes.
	Path string
	// For a delta patch the hash that the existing file should have.
	OldHash string
	// The hash the file should have after patching. If nil the file should be deleted.
	NewHash *string
	// The hash of the full (not delta) patch file.
	CompressedHash string
	// If a delta patch exists the hash of the delta patch file.
	DeltaHash *string
	// Size in bytes of the full patch file.
	FullReplaceSize int64
	// Size in bytes of the delta patch file. Zero if there is no delta patch file.
	DeltaSize int64
}

// DecodeInstructions decodes instructions.json and runs some basic sanity checks.
func DecodeInstructions(jsonData []byte, instructionsHash string) ([]Instruction, error) {
	checksum := HashBytes(jsonData)
	if !strings.EqualFold(checksum, instructionsHash) {
		return nil, fmt.Errorf("instructions.json hash mismatch, expected %s got %s", instructionsHash, checksum)
	}
	var rawInstructions []rawInstruction
	if err := json.Unmarshal(jsonData, &rawInstructions); err != nil {
		return nil, fmt.Errorf("instructions.json couldn't be decoded: %s", err)
	}
	instructions := make([]Instruction, 0, len(rawInstructions))
	for _, ri := range rawInstructions {
		// This little dance normalizes paths to work on Linux as well.
		path := filepath.Clean(strings.ReplaceAll(ri.Path, "\\", string(filepath.Separator)))
		if filepath.IsAbs(path) {
			return nil, fmt.Errorf("instructions.json contains absolute path: %s", path)
		}
		// Prevent escapes via stuff like '..', assuming the directory doesn't already have weird stuff like
		// symlinked directories.
		if !filepath.IsLocal(path) {
			return nil, fmt.Errorf("instructions.json contains non-local path: %s", path)
		}

		if ri.HasDelta && ri.DeltaHash == nil {
			return nil, fmt.Errorf("instructions.json has HasDelta set but no DeltaHash for %s", path)
		}
		if !ri.HasDelta && ri.DeltaHash != nil {
			return nil, fmt.Errorf("instructions.json has HasDelta unset but contains a DeltaHash for %s", path)
		}

		instructions = append(instructions, Instruction{
			Path:            path,
			OldHash:         ri.OldHash,
			NewHash:         ri.NewHash,
			CompressedHash:  ri.CompressedHash,
			DeltaHash:       ri.DeltaHash,
			FullReplaceSize: ri.FullReplaceSize,
			DeltaSize:       ri.DeltaSize,
		})
	}
	return instructions, nil
}
