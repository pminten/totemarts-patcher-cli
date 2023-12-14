package patcher

import (
	"encoding/json"
	"fmt"
	"net/url"
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
}

// FullPatchPath constructs the path to the full patch file given a base URL.
// If there's no patch file (i.e. if the file should be deleted) the second
// return value is false.
func (i *Instruction) FullPatchPath(base *url.URL) (*url.URL, bool) {
	if i.NewHash == nil {
		return nil, false
	}
	return base.JoinPath("full", *i.NewHash), true
}

// DeltaPatchPath constructs the path to the delta hash file.
// If there's no patch file (i.e. if the file should be deleted) or
// there's no delta patch the second return value is false.
func (i *Instruction) DeltaPatchPath(base *url.URL) (*url.URL, bool) {
	if filename, ok := i.DeltaPatchFilename(); ok {
		return base.JoinPath("delta", filename), true
	}
	return nil, false
}

// DeltaPatchFilename returns a filename for the delta hash file.
// If there's no patch file (i.e. if the file should be deleted) or
// there's no delta patch the second return value is false.
func (i *Instruction) DeltaPatchFilename() (string, bool) {
	if i.NewHash == nil {
		return "", false
	}
	if i.DeltaHash == nil {
		return "", false
	}
	return fmt.Sprintf("%s_to_%s", i.OldHash, *i.NewHash), true
}

// IsDelete is true if the instruction is a delete instruction.
func (i *Instruction) IsDelete() bool {
	return i.NewHash == nil
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
		path := strings.ReplaceAll(ri.Path, "\\", "/")
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
			Path:           path,
			OldHash:        ri.OldHash,
			NewHash:        ri.NewHash,
			CompressedHash: ri.CompressedHash,
			DeltaHash:      ri.DeltaHash,
		})
	}
	return instructions, nil
}
