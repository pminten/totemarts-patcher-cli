package patcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var errOurTimeout = errors.New("[TA] timeout")
var errOurStall = errors.New("[TA] stalled")

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

	// How much time to allow to send a request and receive the start of a response.
	DownloadRequestTimeout time.Duration

	// How much time to allow between receiving any data in a download.
	DownloadStallTimeout time.Duration
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
	d           *Downloader
	downloadUrl *url.URL

	// Used in some error detection code.
	downloadIdx int64
}

// A downloadObserver is used to track download measurements.
type downloadObserver struct {
	// Corresponding downloadInProgress structure. Everything reachable through this pointer
	// is protected by the main downloader mutex. It is NOT covered by mu.
	dip *downloadRecord

	// Mutex covering all fields below this.
	mu sync.Mutex

	// Hash in progress.
	hash hash.Hash

	// How many seconds have passed without progress being made.
	secondsWithoutData int

	// If true the observer is being used to catch up to the data of an existing file.
	catchUpMode bool
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
		defer ticker.Stop()
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

	observer, err := d.register(downloadUrl, filename, downloadIdx)
	if err != nil {
		return err
	}

	// If the output file already exists try to reuse it, it may be an incomplete download.
	// O_RDWD: Both read and write.
	// O_CREATE: If it doesn't exist yet create it.
	// No O_APPEND: it would interfere with partially reading the file (which is necessary for hashing).
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return fmt.Errorf("failed to open '%s' for downloading '%s' into: %w", filename, downloadUrl, err)
	}
	defer file.Close()

	// Read all the bytes from the file. As a side effect this sets the file position at the end
	// so writes go to the correct place.
	offset, err := io.Copy(observer, file)
	if err != nil {
		return fmt.Errorf("failed to read current data from '%s': %w", filename, err)
	}
	if offset == expectedSize {
		actualChecksum := observer.getChecksum()
		if HashEqual(expectedChecksum, actualChecksum) {
			log.Printf("Found previous completed download of '%s' (from '%s'), skipping download.",
				filename, downloadUrl)
			return nil
		} else {
			log.Printf(
				`Previous completed download of '%s' (from '%s') has invalid checksum (expected %s, got %s), `+
					`redownloading.`,
				filename, downloadUrl, expectedChecksum, actualChecksum)
			observer.resetChecksum()
			if err := file.Truncate(0); err != nil {
				return fmt.Errorf("failed to truncate '%s': %w", filename, err)
			}
		}
	} else if offset > expectedSize {
		log.Printf(
			`Previous completed download of '%s' (from '%s') has too large size (expected %d, got %d), `+
				`redownloading.`,
			filename, downloadUrl, expectedSize, offset)
		observer.resetChecksum()
		if err := file.Truncate(0); err != nil {
			return fmt.Errorf("failed to truncate '%s': %w", filename, err)
		}
	} else if offset > 0 {
		log.Printf("Found partial (%d/%d bytes) download of '%s' (from '%s'), resuming download.",
			offset, expectedSize, filename, downloadUrl)
	}

	observer.setCatchUpMode(false)

	waitTime := config.RetryBaseDelay
	attempt := 1
	for {
		if newOffset, err := d.doDownloadFile(
			ctx,
			file,
			observer,
			downloadUrl,
			filename,
			expectedChecksum,
			expectedSize,
			offset,
			downloadIdx,
		); err != nil {
			offset = newOffset
			if attempt > config.MaxAttempts {
				return err
			}
			if errors.Is(err, context.Canceled) {
				// Don't log cancelations, those likely aren't errors.
				return err
			}
			// URL is already in the error message, probably twice, no need to add it here.
			log.Printf("Download failed [attempt %d/%d, waiting %s until next attempt]: %s",
				attempt, config.MaxAttempts, waitTime, err)

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
// Returns how many bytes have been written to the file in total, even if an error is returned.
func (d *Downloader) doDownloadFile(
	ctx context.Context,
	file *os.File,
	observer *downloadObserver,
	downloadUrl *url.URL,
	filename string,
	expectedChecksum string,
	expectedSize int64,
	offset int64, // Starting point for downloading new data.
	downloadIdx int64,
) (int64, error) {
	possComplete := ""
	if offset > 0 {
		possComplete = " complete"
	}
	if offset >= expectedSize { // Sanity check.
		return offset, fmt.Errorf(
			"invalid offset %d for '%s', would end up requesting more than size (%d)",
			offset, downloadUrl, expectedSize)
	}

	requestCtx, cancelRequestCtx := context.WithCancelCause(ctx)

	// This is for canceling the Do part, i.e. sending the request and reading the response headers
	// (and a small bit of the response). It is similar to http.Client.Timeout but that doesn't work well
	// here because it also covers the entire response body read time and that can be very significant.
	doCtx, cancelDoCtx := context.WithCancelCause(requestCtx)
	doDoneChan := make(chan struct{})
	go func() {
		select {
		case <-time.After(d.config.DownloadRequestTimeout):
			cancelDoCtx(errOurTimeout)
		case <-doDoneChan:
		}
	}()
	req, err := http.NewRequestWithContext(doCtx, http.MethodGet, downloadUrl.String(), nil)
	if err != nil {
		return offset, fmt.Errorf("failed to create request to download '%s': %w", downloadUrl, err)
	}
	if offset > 0 {
		// Endpoint of range is inclusive.
		req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", offset, expectedSize-1))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Higher levels of the code treat a cancellation error as normal, figuring someone might
		// have pressed interrupt or something. By detecting this and explicitly setting the error
		// to a non-canceled error this is avoided.
		if errors.Is(err, context.Canceled) && errors.Is(context.Cause(doCtx), errOurTimeout) {
			err = fmt.Errorf("failed to request%s download of '%s': request timeout (%s) exceeded",
				possComplete, downloadUrl, d.config.DownloadRequestTimeout)
		}
		return offset, fmt.Errorf("failed to request%s download of '%s': %w", possComplete, downloadUrl, err)
	}
	close(doDoneChan)
	defer resp.Body.Close()

	if offset > 0 {
		if resp.StatusCode == http.StatusOK {
			return offset, fmt.Errorf(
				"failed to resume download '%s' to '%s', server doesn't understand range header (status 200)",
				downloadUrl, filename)
		}
		if resp.StatusCode != http.StatusPartialContent {
			return offset, fmt.Errorf("failed to resume download '%s' (status %d)",
				downloadUrl, resp.StatusCode)
		}
	} else {
		if resp.StatusCode != http.StatusOK {
			return offset, fmt.Errorf("failed to download '%s' (status %d)",
				downloadUrl, resp.StatusCode)
		}
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/octet-stream" {
		// Hopefully this will protect against crazy MitM ISPs injecting weird errors.
		return offset, fmt.Errorf("failed to%s download '%s': unexpected content type %q, expected %s",
			possComplete, downloadUrl, contentType, "application/octet-stream")
	}

	watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
	defer cancelWatchdog()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Can avoid defer Unlock because all actions are simple and can't fail.
				observer.mu.Lock()
				secsWithoutData := observer.secondsWithoutData
				observer.secondsWithoutData++
				observer.mu.Unlock()
				if time.Duration(secsWithoutData)*time.Second > d.config.DownloadStallTimeout {
					cancelRequestCtx(errOurStall)
				}
			case <-watchdogCtx.Done():
				return
			}
		}
	}()

	reader := io.TeeReader(resp.Body, observer)
	written, err := io.Copy(file, reader)
	offset += written
	if err != nil {
		if errors.Is(err, context.Canceled) && errors.Is(context.Cause(doCtx), errOurStall) {
			err = fmt.Errorf("failed to%s download '%s' to '%s': download stalled for at least %s",
				possComplete, downloadUrl, filename, d.config.DownloadStallTimeout)
		}
		return offset, fmt.Errorf("failed to%s download '%s' to '%s': %w", possComplete, downloadUrl, filename, err)
	}

	if offset < expectedSize {
		return offset, fmt.Errorf(
			`failed to%s download '%s' to '%s': download stopped before file was fully received `+
				`(got %d, need %d bytes)`, possComplete, downloadUrl, filename, offset, expectedSize)
	}

	actualChecksum := observer.getChecksum()
	if !HashEqual(expectedChecksum, actualChecksum) {
		return offset,
			fmt.Errorf("downloaded file has invalid checksum for '%s' downloaded to '%s', expected %s, got %s",
				downloadUrl, filename, expectedChecksum, actualChecksum)
	}
	return offset, nil
}

func (d *Downloader) register(downloadUrl *url.URL, filename string, downloadIdx int64) (*downloadObserver, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	existing, found := d.downloads[filename]
	// If downloadIdx is the same it's a retry of the same download, not a new one.
	if found && existing.downloadIdx != downloadIdx {
		// Error message assumes register is only called from DownloadFile, which it should be.
		return nil, fmt.Errorf("DownloadFile called twice for '%s'", filename)
	}
	dip := &downloadRecord{
		d:           d,
		downloadUrl: downloadUrl,
		downloadIdx: downloadIdx,
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
	// Not using defer here to avoid the two mutexes being locked at the same time.
	// While it shouldn't deadlock keeping lock regions small and non-overlapping simplifies
	// reasoning about them.
	o.mu.Lock()
	o.hash.Write(p)
	o.secondsWithoutData = 0
	o.mu.Unlock()

	if !o.catchUpMode {
		o.dip.d.mu.Lock()
		defer o.dip.d.mu.Unlock()

		count := int64(len(p))
		o.dip.d.bytesDownloadedThisSecond += count
		o.dip.d.bytesDownloadedTotal += count
	}

	return len(p), nil
}

// setCatchUpMode enables or disables catch up mode (which makes the observer only add new data to the hash).
func (o *downloadObserver) setCatchUpMode(catchUpMode bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.catchUpMode = catchUpMode
}

// getChecksum returns the checksum computed so far from the observer.
func (o *downloadObserver) getChecksum() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return hex.EncodeToString(o.hash.Sum(nil))
}

// resetChecksum resets the hash inside the observer.
func (o *downloadObserver) resetChecksum() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.hash = sha256.New()
}
