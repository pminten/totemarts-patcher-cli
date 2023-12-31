package main

import (
	"fmt"
	"time"

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
func makeFancyProgressFunc(product string, installDir string, gameVersion *string) (func(patcher.Progress), func()) {
	var widecounters pb.ElementFunc = func(state *pb.State, args ...string) string {
		current := state.Current()
		total := state.Total()
		if state.GetBool("force_zero") {
			// The progress bar lib doesn't understand "0 total elements, but 100% complete"
			// (used for phase completed). The workaround is to set current and total to 1.
			// To avoid the user noticing the counters are faked.
			current = 0
			total = 0
		}
		totalStr := "?"
		if state.GetBool("total_known") {
			totalStr = fmt.Sprintf("%d", total)
		}
		return fmt.Sprintf("%5d / %5s", current, totalStr)
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

	var duration pb.ElementFunc = func(state *pb.State, args ...string) string {
		d := state.Get("duration").(time.Duration)
		return fmt.Sprintf("%01d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	}
	pb.RegisterElement("duration", duration, false)

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
				`{{bar . "[" "=" ">" "_" "]" }} {{wideperc . }} [{{duration .}}]`).
			Set("prefix", pbi.t)
		if err := bar.Err(); err != nil {
			panic(fmt.Sprintf("Failed to set bar template: %s", err))
		}
		phaseBars[pbi.ph] = bar
	}

	titleTemplate := `Installing or updating game '{{string . "product"}}' `
	if gameVersion != nil {
		titleTemplate += `to '{{string . "gameVersion"}}' `
	}
	titleTemplate += `at '{{string . "installDir"}}'`
	titleBar := pb.New(0).
		SetTemplateString(titleTemplate).
		Set("product", product).
		Set("installDir", installDir)
	if gameVersion != nil {
		titleBar.Set("gameVersion", *gameVersion)
	}

	statsBar := pb.New(0).
		SetTemplateString(`({{with string . "prefix"}}{{.}} {{end}}{{downloadstats .}})`).
		Set("prefix", "Download speed: ").
		Set("speed", 0).
		Set("bytesTotal", 0)
	if err := statsBar.Err(); err != nil {
		panic(fmt.Sprintf("Failed to set bar template: %s", err))
	}

	allBars := make([]*pb.ProgressBar, 0, len(phaseBars)+2)
	allBars = append(allBars, titleBar)
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
			phb.Set("duration", time.Duration(ph.Duration)*time.Second)
			phb.Set("total_known", ph.NeededKnown)
			if ph.Done {
				if ph.Needed == 0 {
					// Fake the amounts to get 100% bar.
					phb.SetCurrent(1)
					phb.SetTotal(1)
					phb.Set("force_zero", true)
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
	phaseTime := func(pp patcher.ProgressPhase) string {
		d := time.Duration(pp.Duration) * time.Second
		if d < 1*time.Hour {
			return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
		} else {
			return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
		}
	}
	phaseProgress := func(ph patcher.ProgressPhase) string {
		var perc float64
		if ph.Needed > 0 {
			perc = float64(ph.Completed) / float64(ph.Needed) * 100
		} else {
			perc = 0
		}
		neededStr := "?"
		if ph.NeededKnown {
			neededStr = fmt.Sprintf("%d", ph.Needed)
		}
		if ph.Processing > 0 {
			return fmt.Sprintf("%d/%s (%.1f%%, %s, %d in progress)", ph.Completed, neededStr, perc,
				phaseTime(ph), ph.Processing)
		} else if ph.Done {
			return fmt.Sprintf("%d/%s (100%%, %s)", ph.Completed, neededStr, phaseTime(ph))
		} else {
			return fmt.Sprintf("%d/%s (%.1f%%, %s)", ph.Completed, neededStr, perc, phaseTime(ph))
		}
	}
	fmt.Printf("Verify: %s, Download: %s, Apply: %s, DL: %s/s, %s total\n",
		phaseProgress(p.Verify), phaseProgress(p.Download), phaseProgress(p.Apply),
		byteStr(p.DownloadSpeed), byteStr(p.DownloadTotalBytes))
}
