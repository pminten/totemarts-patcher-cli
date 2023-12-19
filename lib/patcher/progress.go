package patcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	DownloadSpeed int64 `json:"download_speed"`

	// Total bytes downloaded.
	DownloadTotalBytes int64 `json:"download_total_bytes"`

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
}

// NewProgress creates a progress tracker.
func NewProgress() *ProgressTracker {
	return &ProgressTracker{}
}

// Current returns a copy of the current progress.
func (p *ProgressTracker) Current() Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current
}

// UpdateDownloadStats updates the download related statistics.
func (p *ProgressTracker) UpdateDownloadStats(stats DownloadStats) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current.DownloadSpeed = stats.Speed
	p.current.DownloadTotalBytes = stats.TotalBytes
}

// SetPhaseNeeded sets the needed value for a phase.
func (p *ProgressTracker) SetPhaseNeeded(phase Phase, needed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current.GetPhase(phase).Needed = needed
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
	// Canceled is not completed but neither is it an error.
}

// PhaseDone marks a phase as finished.
func (p *ProgressTracker) PhaseDone(phase Phase) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current.GetPhase(phase).Done = true
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
