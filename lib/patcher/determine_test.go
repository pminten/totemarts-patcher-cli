package patcher

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	// Because of filepath separator normalization in the code putting these in directly
	// would make the tests fail on Windows or Linux.
	filename1 = filepath.Join("a", "b")
	filename2 = filepath.Join("a", "c")

	date1 = time.Date(2023, 15, 12, 16, 50, 42, 622, time.UTC)
	date2 = time.Date(2023, 15, 12, 16, 55, 12, 232, time.UTC)
)

func TestDetermineFilesToMeasureFoundInManifest(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: someStr("ghi"),
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure, checksums := DetermineFilesToMeasure(instructions, manifest, infos)
	require.Empty(t, toMeasure)
	require.EqualValues(t, map[string]string{filename1: "def"}, checksums)
}

func TestDetermineFilesToMeasureNotFoundInManifest(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: someStr("ghi"),
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename2, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure, checksums := DetermineFilesToMeasure(instructions, manifest, infos)
	require.EqualValues(t, []string{filename1}, toMeasure)
	require.Empty(t, checksums)
}

func TestDetermineFilesToMeasureFoundInManifestWithWrongTimestamp(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: someStr("ghi"),
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date2},
	}
	toMeasure, checksums := DetermineFilesToMeasure(instructions, manifest, infos)
	require.EqualValues(t, []string{filename1}, toMeasure)
	require.Empty(t, checksums)
}

func TestDetermineFilesToMeasureFoundInManifestWithWrongChecksum(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: someStr("ghi"),
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "efg")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure, checksums := DetermineFilesToMeasure(instructions, manifest, infos)
	require.Empty(t, toMeasure)
	require.EqualValues(t, map[string]string{filename1: "efg"}, checksums)
}

func TestDetermineFilesToMeasureNotFoundInInfos(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: someStr("ghi"),
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename2: {ModTime: date1},
	}
	toMeasure, checksums := DetermineFilesToMeasure(instructions, manifest, infos)
	// If the file is not in infos it doesn't exist yet, so it doesn't need
	// to be verified. It will however need to be downloaded but that's
	// not something this function determines.
	require.Empty(t, toMeasure)
	require.Empty(t, checksums)
}

func TestDetermineFilesOnlyDelete(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        nil,
			CompressedHash: nil,
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure, checksums := DetermineFilesToMeasure(instructions, manifest, infos)
	require.Empty(t, toMeasure)
	require.Empty(t, checksums)
}

func TestDetermineActionsOnlyDelete(t *testing.T) {
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         nil,
			CompressedHash:  nil,
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
		{
			Path:            filename2,
			OldHash:         "zyx",
			NewHash:         nil,
			CompressedHash:  nil,
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	manifest.Add(filename2, date2, "wvu")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	checksums := map[string]string{}
	actions := DetermineActions(instructions, manifest, infos, checksums)
	require.Empty(t, actions.ToDownload)
	require.Empty(t, actions.ToUpdate)
	// Only file 1, file 2 doesn't exist on the filesystem and thus shouldn't be deleted.
	require.EqualValues(t, actions.ToDelete, []string{filename1})
}

func TestDetermineActionsFileNotExists(t *testing.T) {
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         someStr("def"),
			CompressedHash:  someStr("ghi"),
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{}
	checksums := map[string]string{}
	actions := DetermineActions(instructions, manifest, infos, checksums)
	require.EqualValues(t, []DownloadInstr{
		{
			RemotePath: "full/def",
			LocalPath:  "patch/def",
			Checksum:   "ghi",
			Size:       12,
		},
	}, actions.ToDownload)
	require.EqualValues(t, []UpdateInstr{
		{
			FilePath:     filename1,
			PatchPath:    "patch/def",
			TempFilename: "patch/apply/00000_def",
			IsDelta:      false,
			Checksum:     "def",
		},
	}, actions.ToUpdate)
	require.Empty(t, actions.ToDelete)
}

func TestDetermineActionsUpToDateFromMeasurement(t *testing.T) {
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         someStr("def"),
			CompressedHash:  someStr("ghi"),
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
	}
	manifest := NewManifest("foo")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date2},
	}
	checksums := map[string]string{filename1: "def"}
	actions := DetermineActions(instructions, manifest, infos, checksums)
	require.Empty(t, actions.ToDownload)
	require.Empty(t, actions.ToUpdate)
	require.Empty(t, actions.ToDelete)
}

func TestDetermineActionsDownloadFullPatchForMismatch(t *testing.T) {
	// Use two elements to add a callback to sort in mapToSortedSlice to coverage.
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         someStr("def"),
			CompressedHash:  someStr("ghi"),
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
		{
			Path:            filename2,
			OldHash:         "zyx",
			NewHash:         someStr("wvu"),
			CompressedHash:  someStr("tsr"),
			DeltaHash:       nil,
			FullReplaceSize: 25,
			DeltaSize:       0,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def") // Wrong date prevents this from being considered.
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date2},
		filename2: {ModTime: date2},
	}
	// Filename2 isn't in there, that causes its hash to be considered empty which
	// is a mismatch with any real hash and thus causes a download.
	checksums := map[string]string{filename1: "deg"}
	actions := DetermineActions(instructions, manifest, infos, checksums)
	require.EqualValues(t, []DownloadInstr{
		{
			RemotePath: "full/def",
			LocalPath:  "patch/def",
			Checksum:   "ghi",
			Size:       12,
		},
		{
			RemotePath: "full/wvu",
			LocalPath:  "patch/wvu",
			Checksum:   "tsr",
			Size:       25,
		},
	}, actions.ToDownload)
	require.EqualValues(t, []UpdateInstr{
		{
			FilePath:     filename1,
			PatchPath:    "patch/def",
			TempFilename: "patch/apply/00000_def",
			IsDelta:      false,
			Checksum:     "def",
		},
		{
			FilePath:     filename2,
			PatchPath:    "patch/wvu",
			TempFilename: "patch/apply/00001_wvu",
			IsDelta:      false,
			Checksum:     "wvu",
		},
	}, actions.ToUpdate)
	require.Empty(t, actions.ToDelete)
}

func TestDetermineActionsDownloadDeltaPatch(t *testing.T) {
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         someStr("def"),
			CompressedHash:  someStr("ghi"),
			DeltaHash:       someStr("jkl"),
			FullReplaceSize: 12,
			DeltaSize:       4,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def") // Wrong date prevents this from being considered.
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date2},
		filename2: {ModTime: date2},
	}
	checksums := map[string]string{filename1: "abc"}
	actions := DetermineActions(instructions, manifest, infos, checksums)
	require.EqualValues(t, []DownloadInstr{
		{
			RemotePath: "delta/def_from_abc",
			LocalPath:  "patch/def_from_abc",
			Checksum:   "jkl",
			Size:       4,
		},
	}, actions.ToDownload)
	require.EqualValues(t, []UpdateInstr{
		{
			FilePath:     filename1,
			PatchPath:    "patch/def_from_abc",
			TempFilename: "patch/apply/00000_def",
			IsDelta:      true,
			Checksum:     "def",
		},
	}, actions.ToUpdate)
	require.Empty(t, actions.ToDelete)
}
