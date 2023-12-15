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

	// Maximum number of attempts.
	maxAttempts int

	// Minimum time between retries.
	retryBaseDelay time.Duration

	// How much to increment the delay between retries (factor, so 2 = double every retry).
	retryWaitIncrementFactor float64

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

// DownloadStats are current information about the download activity.
type DownloadStats struct {
	// Running average of download speed in bytes/second.
	DownloadSpeed int64

	// Total number of bytes downloaded.
	TotalBytes int64

	// TOtal number of completed downloads.
	TotalDownloads int
}

// A downloadRecord is used to keep track of which files are downloading.
// The main difference with a downloadObserver is that the latter is not protected by
// a mutex (to avoid doing hash calculations under mutex).
type downloadRecord struct {
	d             *Downloader
	downloadUrl   *url.URL
	bytesReceived int64

	// Whether the download is done and successful. Notably doesn't get set on errors.
	done bool

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

// NewDownloader creates a new downloader. Pass retry configuration and over how many seconds
// the download speed should be averaged.
func NewDownloader(
	maxAttempts int,
	retryBaseDelay time.Duration,
	retryWaitIncrementFactor float64,
	downloadSpeedWindow int,
) *Downloader {
	return &Downloader{
		mu:                        sync.Mutex{},
		maxAttempts:               maxAttempts,
		retryBaseDelay:            retryBaseDelay,
		retryWaitIncrementFactor:  retryWaitIncrementFactor,
		downloads:                 make(map[string]*downloadRecord),
		downloadSpeed:             NewAverager(downloadSpeedWindow),
		bytesDownloadedThisSecond: 0,
		bytesDownloadedTotal:      0,
		downloadCount:             0,
	}
}

// Tick performs per second bookkeeping. Should be called once per second.
func (d *Downloader) Tick() DownloadStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.downloadSpeed.Add(float64(d.bytesDownloadedThisSecond))
	d.bytesDownloadedThisSecond = 0
	return DownloadStats{
		DownloadSpeed:  int64(d.downloadSpeed.Average()),
		TotalBytes:     d.bytesDownloadedTotal,
		TotalDownloads: int(d.downloadCount),
	}
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
) error {
	d.mu.Lock()
	downloadIdx := d.downloadCount
	d.downloadCount++
	// Copy variables under mutex.
	maxAttempts := d.maxAttempts
	retryBaseDelay := d.retryBaseDelay
	retryMultiplier := d.retryWaitIncrementFactor
	d.mu.Unlock() // Not with a defer but just getting and incrementing vars can't panic.

	waitTime := retryBaseDelay
	attempt := 1
	for {
		if err := d.doDownloadFile(ctx, downloadUrl, filename, expectedChecksum, downloadIdx); err != nil {
			if attempt > maxAttempts {
				return err
			}
			// Make this JSON reportable?
			log.Info().
				Int("attempt", attempt). // Make it easier for one-based readers.
				Int("max_retries", maxAttempts).
				Stringer("wait_time", waitTime).
				Err(err).
				Msg("download failed, will retry after a short wait")

			// This mainly works because the durations are going to be fairly small so overflows are unlikely.
			waitTime = time.Duration(float64(waitTime) * retryMultiplier)
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

	reader := io.TeeReader(resp.Body, observer)
	_, err = io.Copy(file, reader)
	if err != nil {
		return fmt.Errorf("failed to download %q to %q: %w", downloadUrl, filename, err)
	}

	observer.dip.d.mu.Lock()
	defer observer.dip.d.mu.Lock()
	observer.dip.done = true // At this point we know the download was successful.
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
	}
	d.downloads[filename] = dip
	observer := &downloadObserver{
		dip:  dip,
		hash: sha256.New(),
	}
	return observer, nil
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
