package patcher

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

type PatcherConfig struct {
	// Directory where the game should be installed.
	InstallDir string
}

// Helper tuple for measuring a file.
type measuredFile struct {
	filename string
	checksum string
}

// runVerifyPhase runs the entire verification phase.
// It returns the actions to be taken in later phases.
func runVerifyPhase(
	ctx context.Context,
	instructions []Instruction,
	manifest *Manifest,
	installDir string,
	numWorkers int,
) (*DeterminedActions, error) {
	// TODO: Emit progress, add logs.

	log.Info().
		Str("install_dir", installDir).
		Msg("Scanning files in installation directory.")
	existingFiles, err := ScanFiles(installDir)
	if err != nil {
		return nil, err // ScanFiles adds enough context, no need for fmt.Errorf
	}

	toMeasure := DetermineFilesToMeasure(instructions, manifest, existingFiles)
	log.Info().
		Int("files_to_measure", len(toMeasure)).
		Msg("Computing checksums of files.")

	measuredFiles, err := DoInParallelWithResult[string, measuredFile](
		ctx,
		func(ctx context.Context, filename string) (measuredFile, error) {
			realFilename := filepath.Join(installDir, filename)
			reader, err := os.Open(realFilename)
			if err != nil {
				return measuredFile{}, fmt.Errorf("failed to open %q to compute checksum: %w", realFilename, err)
			}
			checksum, err := HashReader(ctx, reader)
			if err != nil {
				return measuredFile{}, fmt.Errorf("failed to open compute checksum of %q: %w", realFilename, err)
			}
			return measuredFile{filename, checksum}, nil
		},
		toMeasure,
		numWorkers,
	)
	if err != nil {
		return nil, err
	}
	checksums := make(map[string]string, len(toMeasure))
	for _, mf := range measuredFiles {
		checksums[mf.filename] = mf.checksum
	}
	actions := DetermineActions(instructions, manifest, existingFiles, checksums)
	return &actions, nil
}

func runDownloadPhase(
	ctx context.Context,
	toDownload []DownloadInstr,
	installDir string,
	baseUrl *url.URL,
	downloader *Downloader,
	numWorkers int,
) error {
	// TODO: Progress.
	log.Info().
		Str("install_dir", installDir).
		Stringer("base_url", baseUrl).
		Int("files_to_download", len(toDownload)).
		Msg("Downloading patch files.")
	err := DoInParallel(
		ctx,
		func(ctx context.Context, di DownloadInstr) error {
			return downloader.DownloadFile(
				ctx,
				baseUrl.JoinPath(di.RemotePath),
				filepath.Join(installDir, di.LocalPath),
				di.Checksum,
				di.Size,
			)
		},
		toDownload,
		numWorkers,
	)
	if err != nil {
		return err
	}
	return nil
}

func runPatchPhase(
	ctx context.Context,
	toUpdate []UpdateInstr,
	toDelete []string,
	installDir string,
	xdelta *XDelta,
	numWorkers int,
) error {
	// TODO: Progress.
	log.Info().
		Str("install_dir", installDir).
		Int("files_to_patch", len(toUpdate)).
		Msg("Patching files.")
	err := DoInParallel(
		ctx,
		func(ctx context.Context, ui UpdateInstr) error {
			patchPath := filepath.Join(installDir, ui.PatchPath)
			newPath := filepath.Join(installDir, ui.TempFilename)
			if ui.IsDelta {
				oldPath := filepath.Join(installDir, ui.FilePath)
				return xdelta.ApplyDeltaPatch(ctx, oldPath, patchPath, newPath)
			} else {
				return xdelta.ApplyFullPatch(ctx, patchPath, newPath)
			}
		},
		toUpdate,
		numWorkers,
	)
	if err != nil {
		return err
	}

	log.Info().
		Str("install_dir", installDir).
		Int("files_to_move", len(toUpdate)).
		Msg("Moving patched files into place.")
	for _, ui := range toUpdate {
		tempPath := filepath.Join(installDir, ui.TempFilename)
		realPath := filepath.Join(installDir, ui.FilePath)
		if err := os.Rename(tempPath, realPath); err != nil {
			return fmt.Errorf("failed to move patched file %q to %q: %w", tempPath, realPath, err)
		}
	}

	log.Info().
		Str("install_dir", installDir).
		Int("files_to_delete", len(toUpdate)).
		Msg("Deleting obsolete files.")
	for _, path := range toDelete {
		realPath := filepath.Join(installDir, path)
		if err := os.Remove(realPath); err != nil {
			return fmt.Errorf("failed to remove file %q: %w", realPath, err)
		}
	}

	return nil
}

func RunPatcher() {

}
