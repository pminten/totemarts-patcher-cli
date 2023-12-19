package main

import (
	"fmt"

	"github.com/cheggaaa/pb/v3"
	"github.com/pminten/totemarts-patcher-cli/lib/patcher"
)

func byteStr(n int64) string {
	if n < 1<<10 {
		return fmt.Sprintf("%d B", n)
	} else if n < 1<<20 {
		return fmt.Sprintf("%.2f KiB", float64(n)/(1<<10))
	} else if n < 1<<30 {
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	} else {
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	}
}

// makeFancyProgressFunc return a progress function for CLI progress bars and a function to clean up
// the progress bars.
func makeFancyProgressFunc() (func(patcher.Progress), func()) {
	var widecounters pb.ElementFunc = func(state *pb.State, args ...string) string {
		current := state.Current()
		total := state.Total()
		if state.GetBool("forcezero") {
			// The progress bar lib doesn't understand "0 total elements, but 100% complete"
			// (used for phase completed). The workaround is to set current and total to 1.
			// To avoid the user noticing the counters are faked.
			current = 0
			total = 0
		}
		return fmt.Sprintf("%5d / %5d", current, total)
	}
	pb.RegisterElement("widecounters", widecounters, false)
	var wideperc pb.ElementFunc = func(state *pb.State, args ...string) string {
		if state.Total() == 0 {
			return "  0%"
		} else {
			return fmt.Sprintf("%3d%%", int(float64(state.Current())/float64(state.Total())*100))
		}
	}
	pb.RegisterElement("wideperc", wideperc, false)
	var downloadstats pb.ElementFunc = func(state *pb.State, args ...string) string {
		speed := state.Get("speed").(int64)
		bytesTotal := state.Get("bytesTotal").(int64)
		return fmt.Sprintf("%s/s; total: %s", byteStr(speed), byteStr(bytesTotal))
	}
	pb.RegisterElement("downloadstats", downloadstats, false)

	phaseBarInfos := []struct {
		t  string
		ph patcher.Phase
	}{
		{"Verify:   ", patcher.PhaseVerify},
		{"Download: ", patcher.PhaseDownload},
		{"Apply:    ", patcher.PhaseApply},
	}

	phaseBars := make([]*pb.ProgressBar, len(phaseBarInfos))
	for _, pbi := range phaseBarInfos {
		bar := pb.New(0).
			SetTemplateString(`{{with string . "prefix"}}{{.}} {{end}}{{widecounters . }} `+
				`{{bar . "[" "=" ">" "_" "]" }} {{wideperc . }}{{with string . "suffix"}} {{.}}{{end}}`).
			Set("prefix", pbi.t)
		if err := bar.Err(); err != nil {
			panic(fmt.Sprintf("Failed to set bar template: %s", err))
		}
		phaseBars[pbi.ph] = bar
	}

	downloadBar := pb.New(0).
		SetTemplateString(`{{with string . "prefix"}}{{.}} {{end}}{{widecounters . }} {{bar . "[" "=" ">" "_" "]" }} {{wideperc . }}{{with string . "suffix"}} {{.}}{{end}}`).
		Set("prefix", "Download: ")
	if err := downloadBar.Err(); err != nil {
		panic(fmt.Sprintf("Failed to set bar template: %s", err))
	}
	statsBar := pb.New(0).
		SetTemplateString(`({{with string . "prefix"}}{{.}} {{end}}{{downloadstats .}})`).
		Set("prefix", "Download speed: ").
		Set("speed", 0).
		Set("bytesTotal", 0)
	if err := statsBar.Err(); err != nil {
		panic(fmt.Sprintf("Failed to set bar template: %s", err))
	}

	allBars := make([]*pb.ProgressBar, 0, len(phaseBars)+1)
	allBars = append(allBars, phaseBars...)
	allBars = append(allBars, statsBar)
	pool := pb.NewPool(allBars...)
	if err := pool.Start(); err != nil {
		panic(fmt.Sprintf("Failed to start progress bars: %s", err))
	}
	patcherFunc := func(p patcher.Progress) {
		for _, pbi := range phaseBarInfos {
			ph := p.GetPhase(pbi.ph)
			phb := phaseBars[pbi.ph]
			// Can store even for a finished bar, under the hood it's just atomics.
			phb.SetCurrent(int64(ph.Completed))
			phb.SetTotal(int64(ph.Needed))
			if ph.Done {
				if ph.Needed == 0 {
					// Fake the amounts to get 100% bar.
					phb.SetCurrent(1)
					phb.SetTotal(1)
					phb.Set("forcezero", true)
				}
				phb.Finish() // Safe to call multiple times.
			}
		}
		statsBar.Set("speed", p.DownloadSpeed)
		statsBar.Set("bytesTotal", p.DownloadTotalBytes)
	}
	stopFunc := func() {
		if err := pool.Stop(); err != nil {
			panic(fmt.Sprintf("failed to stop progress bars: %s", err))
		}
	}
	return patcherFunc, stopFunc
}

func plainProgress(p patcher.Progress) {
	phaseProgress := func(pp patcher.ProgressPhase) string {
		var perc float64
		if pp.Needed > 0 {
			perc = float64(pp.Completed) / float64(pp.Needed) * 100
		} else {
			perc = 0
		}
		if pp.Processing > 0 {
			return fmt.Sprintf("%d/%d (%.1f%%, %d in progress)", pp.Completed, pp.Needed, perc, pp.Processing)
		} else if pp.Done {
			return fmt.Sprintf("%d/%d (100%%)", pp.Completed, pp.Needed)
		} else {
			return fmt.Sprintf("%d/%d (%.1f%%)", pp.Completed, pp.Needed, perc)
		}
	}
	fmt.Printf("Verify: %s, Download: %s, Apply: %s, DL: %s/s, %s total\n",
		phaseProgress(p.Verify), phaseProgress(p.Download), phaseProgress(p.Apply),
		byteStr(p.DownloadSpeed), byteStr(p.DownloadTotalBytes))
}
