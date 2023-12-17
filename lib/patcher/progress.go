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

// Progress is used to track the progress of the patching process.
type Progress struct {
	mu sync.Mutex

	// Running average of download speed in bytes per second.
	DownloadSpeed int64

	// Total bytes downloaded.
	DownloadTotalBytes int64

	// Progress in the verify phase.
	Verify ProgressPhase

	// Progress in the download phase.
	Download ProgressPhase

	// Progress in the apply phase.
	Apply ProgressPhase
}

// ProgressPhase contains the progress in a particular phase.
type ProgressPhase struct {
	// How many items are being processed.
	Processing int
	// How many items have been successfully processed.
	Completed int
	// How many items have errored.
	Errors int
	// How many items should be processed.
	Needed int
}

func NewProgress() *Progress {
	return &Progress{}
}

func (p *Progress) UpdateDownloadStats(stats DownloadStats) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.DownloadSpeed = stats.Speed
	p.DownloadTotalBytes = stats.TotalBytes
}

// SetPhaseNeeded sets the needed value for a phase.
func (p *Progress) SetPhaseNeeded(phase Phase, needed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch phase {
	case PhaseVerify:
		p.Verify.Needed = needed
	case PhaseDownload:
		p.Download.Needed = needed
	case PhaseApply:
		p.Apply.Needed = needed
	default:
		panic(fmt.Sprintf("Unknown phase %d", phase))
	}
}

// PhaseItemStarted increments the processing value in a phase.
func (p *Progress) PhaseItemStarted(phase Phase) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch phase {
	case PhaseVerify:
		p.Verify.Processing++
	case PhaseDownload:
		p.Download.Processing++
	case PhaseApply:
		p.Apply.Processing++
	default:
		panic(fmt.Sprintf("Unknown phase %d", phase))
	}
}

// PhaseItemDone increments the errors or completed value in a phase
// and decreases the processing value.
func (p *Progress) PhaseItemDone(phase Phase, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var ph *ProgressPhase
	switch phase {
	case PhaseVerify:
		ph = &p.Verify
	case PhaseDownload:
		ph = &p.Download
	case PhaseApply:
		ph = &p.Apply
	default:
		panic(fmt.Sprintf("Unknown phase %d", phase))
	}
	// No protection against going below 0, but it should be obvious
	// to the user and is really just a visual bug.
	ph.Processing--
	if err == nil {
		ph.Completed++
	} else if !errors.Is(err, context.Canceled) {
		ph.Errors--
	}
	// Canceled is not completed but neither is it an error.
}
