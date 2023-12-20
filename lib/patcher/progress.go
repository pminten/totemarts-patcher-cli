package patcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Phase int

const (
	PhaseVerify   Phase = 0
	PhaseDownload Phase = 1
	PhaseApply    Phase = 2
)

// ProgressTracker is used to track the progress of the patching process.
type ProgressTracker struct {
	mu      sync.Mutex
	current Progress
}

// Progress is current progress information.
// Beware that this gets directly serialized for JSON progress output,
type Progress struct {
	// Running average of download speed in bytes per second.
	DownloadSpeed int64 `json:"downloadSpeed"`

	// Total bytes downloaded.
	DownloadTotalBytes int64 `json:"downloadTotalBytes"`

	// Progress in the verify phase.
	Verify ProgressPhase `json:"verify"`

	// Progress in the download phase.
	Download ProgressPhase `json:"download"`

	// Progress in the apply phase.
	Apply ProgressPhase `json:"apply"`
}

// ProgressPhase contains the progress in a particular phase.
type ProgressPhase struct {
	// How many items are being processed.
	Processing int `json:"processing"`
	// How many items have been successfully processed.
	Completed int `json:"completed"`
	// How many items have errored.
	Errors int `json:"errors"`
	// How many items should be processed.
	Needed int `json:"needed"`
	// Whether the phase is completed.
	// In the case of completed == 0 the phase might be not started yet
	// or completed.
	Done bool `json:"done"`
	// How much time was spent in the stage at the last time progress was reported, in seconds.
	Duration int `json:"duration"`
	// When the phase was started (if started). Used to calculate how much time was spent in the stage.
	startedAt *time.Time
}

// NewProgress creates a progress tracker.
func NewProgress() *ProgressTracker {
	return &ProgressTracker{}
}

// Current returns a copy of the current progress with duration calculated correctly.
func (p *ProgressTracker) Current() Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	rv := p.current
	now := time.Now()
	rv.Verify.updateDurationToNow(now)
	rv.Download.updateDurationToNow(now)
	rv.Apply.updateDurationToNow(now)
	return rv
}

// UpdateDownloadStats updates the download related statistics.
func (p *ProgressTracker) UpdateDownloadStats(stats DownloadStats) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current.DownloadSpeed = stats.Speed
	p.current.DownloadTotalBytes = stats.TotalBytes
}

// PhaseStarted sets the needed value for a phase and marks it as started.
func (p *ProgressTracker) PhaseStarted(phase Phase, needed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ph := p.current.GetPhase(phase)
	ph.Needed = needed
	t := time.Now()
	ph.startedAt = &t
}

// PhaseDone marks a phase as finished.
func (p *ProgressTracker) PhaseDone(phase Phase) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ph := p.current.GetPhase(phase)
	ph.Done = true
	ph.Duration = int(time.Since(*ph.startedAt).Seconds())
}

// PhaseItemStarted increments the processing value in a phase.
func (p *ProgressTracker) PhaseItemStarted(phase Phase) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current.GetPhase(phase).Processing++
}

// PhaseItemDone increments the errors or completed value in a phase
// and decreases the processing value.
func (p *ProgressTracker) PhaseItemDone(phase Phase, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ph := p.current.GetPhase(phase)
	// No protection against going below 0, but it should be obvious
	// to the user and is really just a visual bug.
	ph.Processing--
	if err == nil {
		ph.Completed++
	} else if !errors.Is(err, context.Canceled) {
		ph.Errors++
	}
	// No else: canceled is not completed but neither is it an error.
}

// PhaseItemsSkipped increases the completed count for a phase without
// putting items in processing.
func (p *ProgressTracker) PhaseItemsSkipped(phase Phase, count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current.GetPhase(phase).Completed += count
}

// GetPhase returns a phase by number.
func (p *Progress) GetPhase(phase Phase) *ProgressPhase {
	switch phase {
	case PhaseVerify:
		return &p.Verify
	case PhaseDownload:
		return &p.Download
	case PhaseApply:
		return &p.Apply
	default:
		panic(fmt.Sprintf("Unknown phase %d", phase))
	}
}

// updateDuration updates the duration field of a phase if it is dependent on the currentTime.
func (pp *ProgressPhase) updateDurationToNow(now time.Time) {
	if pp.startedAt != nil && !pp.Done {
		pp.Duration = int(now.Sub(*pp.startedAt).Seconds())
	}
}
