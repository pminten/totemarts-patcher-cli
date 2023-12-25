package patcher

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// The patching process consists of four phases of I/O mixed with two phases of thinking.
// 1. Scan all the existing files in the installation dir.
// a. Determine files to be verified.
// 2. Verify files.
// b. Determine what to do with files (update, delete).
// 3. Download patch files.
// 4. Apply patch files and delete files (i.e. create/update/delete final files).
//
// For historical reasons scanning and verifying is considered to be part of a single phase.
// The user probably won't notice, the scanning part is pretty quick given that there are only
// in the order of 1000 files.

// A DownloadInstr indicates how to download a patch file.
type DownloadInstr struct {
	// Location of the patch file relative to the directory containing instructions.json.
	RemotePath string

	// Filename for the patch file on disk.
	LocalPath string

	// Checksum the patch file should have.
	Checksum string

	// Size of the patch file in bytes.
	Size int64
}

// An UpdateInstr indicates how to apply a patch.
type UpdateInstr struct {
	// Filename for the patch file on disk.
	PatchPath string

	// Filename for the file to patch.
	FilePath string

	// Temporary filename for the new file.
	TempFilename string

	// Whether the patch should be applied as a delta patch.
	IsDelta bool

	// The final hash the file should have.
	Checksum string

	// The size the file is expected to have. It may be 0 if this is unknown.
	Size int64
}

// DeterminedActions are the result of DetermineActions.
type DeterminedActions struct {
	// Which patch files to download.
	ToDownload []DownloadInstr
	// Which game files to install/update.
	ToUpdate []UpdateInstr
	// Which game files to delete.
	ToDelete []string
}

// DetermineFilesToVerify given the raw instructions, a manifest (empty if it doesn't exist yet) and
// metadata of existing files returns a list of files that should be measured (checksum taken)
// and hashes of existing files that match the manifest.
func DetermineFilesToMeasure(
	instructions []Instruction,
	manifest *Manifest,
	existingFiles map[string]BasicFileInfo,
) ([]string, map[string]string) {
	toVerify := make([]string, 0)
	checksums := make(map[string]string)
	// A file should be measured if it is needed but can't be checked in the manifest.
	for _, instr := range instructions {
		if instr.NewHash == nil {
			// Delete instruction so no need to measure anything.
			continue
		}
		fileInfo, found := existingFiles[instr.Path]
		if !found {
			continue // Can't measure something that doesn't exist.
		}
		if cs, found := manifest.Get(instr.Path, fileInfo.ModTime); found {
			checksums[instr.Path] = cs
		} else {
			toVerify = append(toVerify, instr.Path)
		}
	}
	return toVerify, checksums
}

// DetermineActions determines what should be downloaded and what should be patched/deleted.
// It receives the same data as DetermineFilesToMeasure and additionally the combined results
// of result of file measurement and looking up files in the manifest.
func DetermineActions(
	instructions []Instruction,
	manifest *Manifest,
	existingFiles map[string]BasicFileInfo,
	fileChecksums map[string]string,
) DeterminedActions {
	toDownloadMap := make(map[string]DownloadInstr) // Keyed by CompressedHash or DeltaHash.
	toUpdateMap := make(map[string]UpdateInstr)     // Keyed by Path
	toDelete := make([]string, 0)

	for instrIdx, instr := range instructions {
		_, found := existingFiles[instr.Path]
		// If NewHash is nil CompressedHash should be nil as well and vice versa.
		// The extra check for CompressedHash is mainly here to guard against corrupted files causing panics.
		if instr.NewHash == nil || instr.CompressedHash == nil {
			if found {
				toDelete = append(toDelete, instr.Path)
			}
			continue
		}

		// Note: path, not filepath, so the slashes don't get replaced by backslashes.
		fullPatchRemotePath := path.Join("full", *instr.NewHash)
		fullPatchLocalPath := path.Join("patch", *instr.NewHash)

		// The temp files get moved into place, which causes problems if a single applied
		// file is used for multiple final files. So use a naming scheme that avoid such
		// complications. The index refers to the index in the instructions.json file.
		tempPath := path.Join("patch", "apply", fmt.Sprintf("%05d_%s", instrIdx, *instr.NewHash))

		if found && HashEqual(fileChecksums[instr.Path], *instr.NewHash) {
			continue // Already up to date.
		} else if found && instr.DeltaHash != nil && HashEqual(fileChecksums[instr.Path], instr.OldHash) {
			// Can use (hopefully much smaller) delta file to upgrade.
			deltaFilename := fmt.Sprintf("%s_from_%s", *instr.NewHash, instr.OldHash)
			deltaPatchRemotePath := path.Join("delta", deltaFilename)
			deltaPatchLocalPath := path.Join("patch", deltaFilename)
			toDownloadMap[*instr.DeltaHash] = DownloadInstr{
				RemotePath: deltaPatchRemotePath,
				LocalPath:  deltaPatchLocalPath,
				Checksum:   *instr.DeltaHash,
				Size:       instr.DeltaSize,
			}
			toUpdateMap[instr.Path] = UpdateInstr{
				FilePath:     instr.Path,
				PatchPath:    deltaPatchLocalPath,
				TempFilename: tempPath,
				IsDelta:      true,
				Checksum:     *instr.NewHash,
				Size:         instr.FileSize,
			}
		} else {
			// File doesn't match checksum or doesn't exist yet.
			toDownloadMap[*instr.CompressedHash] = DownloadInstr{
				RemotePath: fullPatchRemotePath,
				LocalPath:  fullPatchLocalPath,
				Checksum:   *instr.CompressedHash,
				Size:       instr.FullReplaceSize,
			}
			toUpdateMap[instr.Path] = UpdateInstr{
				FilePath:     instr.Path,
				PatchPath:    fullPatchLocalPath,
				TempFilename: tempPath,
				IsDelta:      false,
				Checksum:     *instr.NewHash,
				Size:         instr.FileSize,
			}
		}
	}
	sort.Slice(toDelete, func(i, j int) bool { return strings.Compare(toDelete[i], toDelete[j]) < 0 })
	return DeterminedActions{
		ToDownload: mapToSortedSlice(toDownloadMap),
		ToUpdate:   mapToSortedSlice(toUpdateMap),
		ToDelete:   toDelete,
	}
}

func mapToSortedSlice[V any](m map[string]V) []V {
	tupleSlice := make([]struct {
		k string
		v V
	}, 0, len(m))
	for k, v := range m {
		tupleSlice = append(tupleSlice, struct {
			k string
			v V
		}{k, v})
	}
	sort.Slice(tupleSlice, func(i, j int) bool {
		return strings.Compare(tupleSlice[i].k, tupleSlice[j].k) < 0
	})
	slice := make([]V, 0, len(m))
	for _, t := range tupleSlice {
		slice = append(slice, t.v)
	}
	return slice
}
