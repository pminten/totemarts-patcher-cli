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
			CompressedHash: "ghi",
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure := DetermineFilesToMeasure(instructions, manifest, infos)
	require.Empty(t, toMeasure)
}

func TestDetermineFilesToMeasureNotFoundInManifest(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: "ghi",
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename2, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure := DetermineFilesToMeasure(instructions, manifest, infos)
	require.EqualValues(t, []string{filename1}, toMeasure)
}

func TestDetermineFilesToMeasureFoundInManifestWithWrongTimestamp(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: "ghi",
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date2},
	}
	toMeasure := DetermineFilesToMeasure(instructions, manifest, infos)
	require.EqualValues(t, []string{filename1}, toMeasure)
}

func TestDetermineFilesToMeasureFoundInManifestWithWrongChecksum(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: "ghi",
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "efg")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure := DetermineFilesToMeasure(instructions, manifest, infos)
	require.EqualValues(t, []string{filename1}, toMeasure)
}

func TestDetermineFilesToMeasureNotFoundInInfos(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        someStr("def"),
			CompressedHash: "ghi",
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename2: {ModTime: date1},
	}
	toMeasure := DetermineFilesToMeasure(instructions, manifest, infos)
	// If the file is not in infos it doesn't exist yet, so it doesn't need
	// to be verified. It will however need to be downloaded but that's
	// not something this function determines.
	require.Empty(t, toMeasure)
}

func TestDetermineFilesOnlyDelete(t *testing.T) {
	instructions := []Instruction{
		{
			Path:           filename1,
			OldHash:        "abc",
			NewHash:        nil,
			CompressedHash: "ghi",
			DeltaHash:      nil,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	toMeasure := DetermineFilesToMeasure(instructions, manifest, infos)
	require.Empty(t, toMeasure)
}

func TestDetermineActionsOnlyDelete(t *testing.T) {
	// Use two elements to cause the callback to a the sort of toDelete
	// to be considered visited in coverage.
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         nil,
			CompressedHash:  "ghi",
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
		{
			Path:            filename2,
			OldHash:         "zyx",
			NewHash:         nil,
			CompressedHash:  "tsr",
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
		filename2: {ModTime: date2},
	}
	checksums := map[string]string{}
	actions := DetermineActions(instructions, manifest, infos, checksums)
	require.Empty(t, actions.ToDownload)
	require.Empty(t, actions.ToUpdate)
	require.EqualValues(t, actions.ToDelete, []string{filename1, filename2})
}

func TestDetermineActionsFileNotExists(t *testing.T) {
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         someStr("def"),
			CompressedHash:  "ghi",
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
			RemotePath:       "full/def",
			LocalPath:        "patch/def",
			ExpectedChecksum: "ghi",
			Size:             12,
		},
	}, actions.ToDownload)
	require.EqualValues(t, []UpdateInstr{
		{
			FilePath:  filename1,
			PatchPath: "patch/def",
			IsDelta:   false,
		},
	}, actions.ToUpdate)
	require.Empty(t, actions.ToDelete)
}

func TestDetermineActionsUpToDateFromManifest(t *testing.T) {
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         someStr("def"),
			CompressedHash:  "ghi",
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
	}
	manifest := NewManifest("foo")
	manifest.Add(filename1, date1, "def")
	infos := map[string]BasicFileInfo{
		filename1: {ModTime: date1},
	}
	checksums := map[string]string{}
	actions := DetermineActions(instructions, manifest, infos, checksums)
	require.Empty(t, actions.ToDownload)
	require.Empty(t, actions.ToUpdate)
	require.Empty(t, actions.ToDelete)
}

func TestDetermineActionsUpToDateFromMeasurement(t *testing.T) {
	instructions := []Instruction{
		{
			Path:            filename1,
			OldHash:         "abc",
			NewHash:         someStr("def"),
			CompressedHash:  "ghi",
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
			CompressedHash:  "ghi",
			DeltaHash:       nil,
			FullReplaceSize: 12,
			DeltaSize:       0,
		},
		{
			Path:            filename2,
			OldHash:         "zyx",
			NewHash:         someStr("wvu"),
			CompressedHash:  "tsr",
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
			RemotePath:       "full/def",
			LocalPath:        "patch/def",
			ExpectedChecksum: "ghi",
			Size:             12,
		},
		{
			RemotePath:       "full/wvu",
			LocalPath:        "patch/wvu",
			ExpectedChecksum: "tsr",
			Size:             25,
		},
	}, actions.ToDownload)
	require.EqualValues(t, []UpdateInstr{
		{
			FilePath:  filename1,
			PatchPath: "patch/def",
			IsDelta:   false,
		},
		{
			FilePath:  filename2,
			PatchPath: "patch/wvu",
			IsDelta:   false,
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
			CompressedHash:  "ghi",
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
			RemotePath:       "delta/abc_to_def",
			LocalPath:        "patch/abc_to_def",
			ExpectedChecksum: "jkl",
			Size:             4,
		},
	}, actions.ToDownload)
	require.EqualValues(t, []UpdateInstr{
		{
			FilePath:  filename1,
			PatchPath: "patch/abc_to_def",
			IsDelta:   true,
		},
	}, actions.ToUpdate)
	require.Empty(t, actions.ToDelete)
}