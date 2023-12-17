package patcher

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

type PatcherConfig struct {
	// URL containing instructions.json
	BaseUrl *url.URL

	// Directory where the game should be installed.
	InstallDir string

	// Product name that should be stored in the manifest.
	Product string

	// How many concurrent workers in verify phase.
	VerifyWorkers int

	// How many concurrent workers in download phase.
	DownloadWorkers int

	// How many concurrent workers in apply phase.
	ApplyWorkers int

	// Configuration of the download system.
	DownloadConfig DownloadConfig

	// Where to find the xdelta binary. If just a basename without directory
	// will look in PATH and also in the current directory.
	XDeltaBinPath string

	// A function that gets called every few seconds with the current progress
	// until the context passed to RunPatcher is canceled.
	ProgressFunc func(Progress)

	// How often to call ProgressFunc.
	ProgressInterval time.Duration
}

// Helper tuple for measuring a file.
type measuredFile struct {
	filename string
	checksum string
	modTime  time.Time
}

// runVerifyPhase runs the entire verification phase.
// It returns the actions to be taken in later phases.
func runVerifyPhase(
	ctx context.Context,
	instructions []Instruction,
	manifest *Manifest,
	installDir string,
	numWorkers int,
	progress *ProgressTracker,
) (*DeterminedActions, error) {
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

	progress.SetPhaseNeeded(PhaseVerify, len(toMeasure))
	measuredFiles, err := DoInParallelWithResult[string, measuredFile](
		ctx,
		func(ctx context.Context, filename string) (mf measuredFile, retErr error) {
			progress.PhaseItemStarted(PhaseVerify)
			defer progress.PhaseItemDone(PhaseVerify, retErr)
			realFilename := filepath.Join(installDir, filename)
			file, err := os.Open(realFilename)
			if err != nil {
				return measuredFile{}, fmt.Errorf("failed to open %q to compute checksum: %w", realFilename, err)
			}
			defer file.Close()
			fileInfo, err := file.Stat()
			if err != nil {
				return measuredFile{}, fmt.Errorf("failed to get basic metadata of %q: %w", realFilename, err)
			}
			checksum, err := HashReader(ctx, file)
			if err != nil {
				return measuredFile{}, fmt.Errorf("failed to compute checksum of %q: %w", realFilename, err)
			}

			return measuredFile{filename, checksum, fileInfo.ModTime()}, nil
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
		manifest.Add(mf.filename, mf.modTime, mf.checksum)
	}
	actions := DetermineActions(instructions, manifest, existingFiles, checksums)
	return &actions, nil
}

func runDownloadPhase(
	ctx context.Context,
	toDownload []DownloadInstr,
	installDir string,
	baseUrl *url.URL,
	downloadConfig DownloadConfig,
	progress *ProgressTracker,
	numWorkers int,
) error {
	// Stop the downloader automatically.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	downloader := NewDownloader(downloadConfig, func(stats DownloadStats) {
		progress.UpdateDownloadStats(stats)
	}, ctx)

	log.Info().
		Str("install_dir", installDir).
		Stringer("base_url", baseUrl).
		Int("files_to_download", len(toDownload)).
		Msg("Downloading patch files.")
	progress.SetPhaseNeeded(PhaseDownload, len(toDownload))
	err := DoInParallel(
		ctx,
		func(ctx context.Context, di DownloadInstr) (retErr error) {
			progress.PhaseItemStarted(PhaseDownload)
			defer progress.PhaseItemDone(PhaseDownload, retErr)
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
	manifest *Manifest,
	installDir string,
	xdelta *XDelta,
	progress *ProgressTracker,
	numWorkers int,
) error {
	log.Info().
		Str("install_dir", installDir).
		Int("files_to_patch", len(toUpdate)).
		Msg("Patching files.")
	progress.SetPhaseNeeded(PhaseApply, len(toUpdate))
	err := DoInParallel(
		ctx,
		func(ctx context.Context, ui UpdateInstr) (retErr error) {
			progress.PhaseItemStarted(PhaseApply)
			progress.PhaseItemDone(PhaseApply, retErr)
			patchPath := filepath.Join(installDir, ui.PatchPath)
			newPath := filepath.Join(installDir, ui.TempFilename)
			if ui.IsDelta {
				oldPath := filepath.Join(installDir, ui.FilePath)
				return xdelta.ApplyPatch(ctx, &oldPath, patchPath, newPath, ui.Checksum)
			} else {
				return xdelta.ApplyPatch(ctx, nil, patchPath, newPath, ui.Checksum)
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
		realDir := filepath.Dir(realPath)
		if err := os.MkdirAll(realDir, 0755); err != nil {
			return fmt.Errorf("failed to ensure directories for patched file %q exist: %w", realPath, err)
		}
		if err := os.Rename(tempPath, realPath); err != nil {
			return fmt.Errorf("failed to move patched file %q to %q: %w", tempPath, realPath, err)
		}
		fileInfo, err := os.Stat(realPath)
		if err != nil {
			return fmt.Errorf("failed to get basic metadata of %q: %w", realPath, err)
		}

		// File hash is checked during xdelta operations, so it should be safe to add this to the manifest.
		manifest.Add(ui.FilePath, fileInfo.ModTime(), ui.Checksum)
	}

	if len(toDelete) > 0 {
		log.Info().
			Str("install_dir", installDir).
			Int("files_to_delete", len(toDelete)).
			Msg("Deleting obsolete files (if any).")
	}
	for _, path := range toDelete {
		realPath := filepath.Join(installDir, path)
		if err := os.Remove(realPath); err != nil {
			return fmt.Errorf("failed to remove file %q: %w", realPath, err)
		}
	}

	return nil
}

func RunPatcher(ctx context.Context, instructions []Instruction, config PatcherConfig) error {
	xdelta, err := NewXDelta(config.XDeltaBinPath)
	if err != nil {
		return err
	}

	manifest, err := ReadManifest(config.InstallDir, config.Product)
	if err != nil {
		return err
	}

	// These paths are also hardcoded in the determination logic.
	patchDir := filepath.Join(config.InstallDir, "patch")
	patchApplyDir := filepath.Join(config.InstallDir, "patch/apply")

	if err = os.MkdirAll(patchApplyDir, 0755); err != nil {
		return fmt.Errorf("couldn't create patch and patch apply directories %q: %w", patchApplyDir, err)
	}

	progress := NewProgress()
	go func() {
		ticker := time.NewTicker(config.ProgressInterval)
		for {
			select {
			case <-ticker.C:
				config.ProgressFunc(progress.Current())
			case <-ctx.Done():
				// Report progress one last time, usually that's the "all completed" progress.
				config.ProgressFunc(progress.Current())
				return
			}
		}
	}()

	actions, err := runVerifyPhase(
		ctx,
		instructions,
		manifest,
		config.InstallDir,
		config.VerifyWorkers,
		progress,
	)
	if err != nil {
		return err
	}

	err = runDownloadPhase(
		ctx,
		actions.ToDownload,
		config.InstallDir,
		config.BaseUrl,
		config.DownloadConfig,
		progress,
		config.DownloadWorkers,
	)
	if err != nil {
		return err
	}

	err = runPatchPhase(
		ctx,
		actions.ToUpdate,
		actions.ToDelete,
		manifest,
		config.InstallDir,
		xdelta,
		progress,
		config.ApplyWorkers,
	)
	if err != nil {
		return err
	}

	log.Info().
		Str("patch_dir", patchDir).
		Msg("Operation successful, removing downloaded patches.")
	if err := os.RemoveAll(patchDir); err != nil {
		return fmt.Errorf("failed to remove patch dir %q: %w", patchDir, err)
	}

	if err := manifest.WriteManifest(config.InstallDir); err != nil {
		return err
	}

	return nil
}
