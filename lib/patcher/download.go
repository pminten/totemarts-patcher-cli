package patcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// A Downloader manages downloads. Mainly it keeps track of progress and download speed.
type Downloader struct {
	mu sync.Mutex

	// Downloader configuration.
	config DownloadConfig

	// Current and past downloads.
	downloads map[string]*downloadRecord

	// Running average of download speed.
	downloadSpeed *Averager

	// How many bytes have been downloaded in the last second.
	bytesDownloadedThisSecond int64

	// How many bytes have been downloaded in total.
	bytesDownloadedTotal int64

	// How many files have been downloaded.
	downloadCount int64
}

// A DownloadConfig is the configuration for a Downloader.
type DownloadConfig struct {
	// Maximum number of attempts.
	MaxAttempts int

	// Minimum time between retries.
	RetryBaseDelay time.Duration

	// How much to increment the delay between retries (factor, so 2 = double every retry).
	RetryWaitIncrementFactor float64

	// How many seconds to average the download speed over.
	DownloadSpeedWindow int
}

// DownloadStats are current information about the download activity.
type DownloadStats struct {
	// Running average of download speed in bytes/second.
	Speed int64

	// Total number of bytes downloaded.
	TotalBytes int64
}

// A downloadRecord is used to keep track of which files are downloading.
// The main difference with a downloadObserver is that the latter is not protected by
// a mutex (to avoid doing hash calculations under mutex).
type downloadRecord struct {
	d             *Downloader
	downloadUrl   *url.URL
	bytesReceived int64

	// Used in some error detection code.
	downloadIdx int64
}

// A downloadObserver is used to track download measurements.
type downloadObserver struct {
	// Corresponding downloadInProgress structure. Everything reachable through this pointer
	// is protected by the main downloader mutex.
	dip *downloadRecord

	// Hash in progress. NOT protected by the downloader mutex.
	hash hash.Hash
}

// NewDownloader creates a new downloader. Pass configuration and a function that will
// receive the download stats every second. Will run the tick func every second until
// the context is canceled.
func NewDownloader(
	config DownloadConfig,
	tickFunc func(DownloadStats),
	tickFuncCtx context.Context,
) *Downloader {
	d := &Downloader{
		mu:                        sync.Mutex{},
		config:                    config,
		downloads:                 make(map[string]*downloadRecord),
		downloadSpeed:             NewAverager(config.DownloadSpeedWindow),
		bytesDownloadedThisSecond: 0,
		bytesDownloadedTotal:      0,
		downloadCount:             0,
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		for {
			select {
			case <-ticker.C:
				tickFunc(d.tick())
			case <-tickFuncCtx.Done():
				ticker.Stop()
				d.mu.Lock()
				defer d.mu.Unlock()
				// Fake an update to force speed to 0, otherwise it might get stuck at
				// a higher value and that looks silly.
				tickFunc(DownloadStats{
					Speed:      0,
					TotalBytes: d.bytesDownloadedTotal,
				})
				return
			}
		}
	}()
	return d
}

// DownloadFile downloads a file to disk. It also verifies a SHA256 hash.
//
// The caller must guarantee that DownloadFile is never called twice for the same filename
// (this can happen if two output files in the instruction list have the same content).
func (d *Downloader) DownloadFile(
	ctx context.Context,
	downloadUrl *url.URL,
	filename string,
	expectedChecksum string,
	expectedSize int64,
) error {
	d.mu.Lock()
	downloadIdx := d.downloadCount
	d.downloadCount++
	// Copy variables under mutex.
	config := d.config // It's a struct of value types, so this is a copy.
	d.mu.Unlock()      // Not with a defer but just getting and incrementing vars can't panic.

	// First check if the output file already exists and has the right checksum,
	// if so it's a leftover from the previous run that we can reuse.
	// This simple approach does have the limitation that a partially downloaded file
	// is ignored, resulting in a full download again, but it's pretty simple and should
	// avoid most downloads after an aborted run.
	existingFile, err := os.Open(filename)

	// This is an optimistic check, any error just means we can't shortcut.
	// No real clean way to write this, nested if might be the least ugly.
	if err == nil {
		defer existingFile.Close()
		// Checking for expected size avoids reading the whole file if there's no way it can match.
		if fileInfo, err := existingFile.Stat(); err == nil && fileInfo.Size() == expectedSize {
			if cs, err := HashReader(ctx, existingFile); err == nil && strings.EqualFold(cs, expectedChecksum) {
				log.Debug().
					Stringer("download_url", downloadUrl).
					Str("filename", filename).
					Msg("Patch file is already present, skipping download.")
				return nil
			}
		}
	}

	waitTime := config.RetryBaseDelay
	attempt := 1
	for {
		if err := d.doDownloadFile(ctx, downloadUrl, filename, expectedChecksum, downloadIdx); err != nil {
			if attempt > config.MaxAttempts {
				return err
			}
			log.Info().
				Stringer("download_url", downloadUrl).
				Str("filename", filename).
				Int("attempt", attempt). // Make it easier for one-based readers.
				Int("max_attempts", config.MaxAttempts).
				Stringer("wait_time", waitTime).
				Err(err).
				Msg("Download failed, will retry after a short wait.")

			// This mainly works because the durations are going to be fairly small so overflows are unlikely.
			attempt++
			waitTime = time.Duration(float64(waitTime) * config.RetryWaitIncrementFactor)
			select {
			case <-time.After(waitTime):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			return nil
		}
	}
}

// doDownloadFile contains the retryable for DownloadFile. It returns the checksum of the downloaded file.
func (d *Downloader) doDownloadFile(
	ctx context.Context,
	downloadUrl *url.URL,
	filename string,
	expectedChecksum string,
	downloadIdx int64,
) error {
	observer, err := d.register(downloadUrl, filename, downloadIdx)
	if err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to open %q for downloading %q to: %w", filename, downloadUrl, err)
	}
	defer file.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadUrl.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request to download %q: %w", downloadUrl, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request download of %q: %w", downloadUrl, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %q to %q (status %d)", downloadUrl, filename, resp.StatusCode)
	}

	reader := io.TeeReader(resp.Body, observer)
	_, err = io.Copy(file, reader)
	if err != nil {
		return fmt.Errorf("failed to download %q to %q: %w", downloadUrl, filename, err)
	}

	observer.dip.d.mu.Lock()
	defer observer.dip.d.mu.Unlock()
	actualChecksum := hex.EncodeToString(observer.hash.Sum(nil))
	if !strings.EqualFold(expectedChecksum, actualChecksum) {
		return fmt.Errorf("downloaded file has invalid checksum for %q downloaded to %q, expected %s, got %s",
			downloadUrl, filename, expectedChecksum, actualChecksum)
	}
	return nil
}

func (d *Downloader) register(downloadUrl *url.URL, filename string, downloadIdx int64) (*downloadObserver, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	existing, found := d.downloads[filename]
	// If downloadIdx is the same it's a retry of the same download, not a new one.
	if found && existing.downloadIdx != downloadIdx {
		// Error message assumes register is only called from DownloadFile, which it should be.
		return nil, fmt.Errorf("DownloadFile called twice for %q", filename)
	}
	dip := &downloadRecord{
		d:             d,
		downloadUrl:   downloadUrl,
		bytesReceived: 0,
		downloadIdx:   downloadIdx,
	}
	d.downloads[filename] = dip
	observer := &downloadObserver{
		dip:  dip,
		hash: sha256.New(),
	}
	return observer, nil
}

// Perform per-second bookkeeping and return download stats to be propagated.
func (d *Downloader) tick() DownloadStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.downloadSpeed.Add(float64(d.bytesDownloadedThisSecond))
	d.bytesDownloadedThisSecond = 0
	return DownloadStats{
		Speed:      int64(d.downloadSpeed.Average()),
		TotalBytes: d.bytesDownloadedTotal,
	}
}

// Write implements (io.Writer).Write
func (o *downloadObserver) Write(p []byte) (n int, err error) {
	o.hash.Write(p)

	o.dip.d.mu.Lock()
	defer o.dip.d.mu.Unlock()

	count := int64(len(p))
	o.dip.bytesReceived += count
	o.dip.d.bytesDownloadedThisSecond += count
	o.dip.d.bytesDownloadedTotal += count

	return len(p), nil
}
