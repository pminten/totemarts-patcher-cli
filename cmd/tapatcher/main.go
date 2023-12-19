package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/alecthomas/kong"
	"github.com/pminten/totemarts-patcher-cli/lib/patcher"
)

var CLI struct {
	// TODO: About command to print licenses of used libs.
	Update struct {
		Product          string `arg:"" name:"product" help:"Code of the game."`
		BaseUrl          string `arg:"" name:"base-url" help:"URL of \"directory\" containing the instructions.json file."`
		InstallDir       string `arg:"" name:"install-dir" help:"Directory where the game should be."`
		Instructions     string `name:"instructions" short:"I" default:"-" type:"existingfile" help:"Path of instructions.json file or '-' to read them from stdin."`
		InstructionsHash string `name:"instructions-hash" short:"H" help:"SHA256 checksum of the instructions file."`
		VerifyWorkers    int    `name:"verify-workers" help:"Number of current file verifications."`
		DownloadWorkers  int    `name:"download-workers" help:"Number of current patch downloads."`
		ApplyWorkers     int    `name:"apply-workers" help:"Number of current patching processes."`
		XDeltaPath       string `name:"xdelta" default:"xdelta3" help:"Path to xdelta3 binary. If no directory name will also look for this in PATH and the current directory."`

		DownloadMaxAttempts     int           `name:"download-max-attempts" default:"5" help:"How many times to try to download a file."`
		DownloadBaseDelay       time.Duration `name:"download-base-delay" default:"1s" help:"How many seconds to wait between download retries at first."`
		DownloadDelayFactor     float64       `name:"download-delay-factor" default:"1.5" help:"How much to multiply delay between download retries after each retry."`
		DownloadSpeedWindow     int           `name:"download-speed-window" default:"5" help:"How many seconds to average download speed over."`
		DownloadRequestTimemout time.Duration `name:"download-request-timeout" default:"30s" help:"How many seconds to allow before receiving the start of a download response."`
		DownloadStallTimeout    time.Duration `name:"download-stall-timeout" default:"30s" help:"How many seconds to allow between receiving any data in a download."`

		ProgressInterval int    `name:"progress-interval" default:"1" help:"How often to report progress."`
		ProgressMode     string `name:"progress-mode" enum:"plain,fancy,json" default:"fancy" help:"How to report progress (plain, fancy or json)."`

		Verbose       bool   `name:"verbose" short:"v" help:"Use verbose logging."`
		OmitTimestamp bool   `name:"omit-timestamp" help:"Disable timestamps in logs."`
		LogFile       string `name:"log-file" type:"path" help:"Where to store logs. Particularly useful with fancy progress mode as that hides logs."`
	} `cmd:"" help:"Install or update a game."`
	About struct {
	} `cmd:"" help:"Show license info."`
}

func Update() {
	ctx := patcher.SetVerbose(context.Background(), CLI.Update.Verbose)

	if CLI.Update.OmitTimestamp {
		log.SetFlags(0)
	}
	if CLI.Update.LogFile != "" {
		logFile, err := os.Create(CLI.Update.LogFile)
		if err != nil {
			panic(fmt.Sprintf("Couldn't open log file '%s': %v", CLI.Update.LogFile, err))
		}
		log.SetOutput(logFile)
	}
	if CLI.Update.ProgressMode == "fancy" && CLI.Update.LogFile == "" {
		log.SetOutput(io.Discard) // Avoid messing with the terminal while progress is being printed.
	}

	absInstallDir, err := filepath.Abs(CLI.Update.InstallDir)
	if err != nil {
		log.Fatalf("install-dir is not a valid directory name: %s", err)
	}

	baseUrl, err := url.Parse(CLI.Update.BaseUrl)
	if err != nil {
		log.Fatalf("base-url is not a valid URL: %s", err)
	}

	var progressFunc func(patcher.Progress)
	if CLI.Update.ProgressMode == "json" {
		progressFunc = func(p patcher.Progress) {
			data, err := json.Marshal(p)
			if err != nil {
				log.Fatalf("Failed to serialize progress structure: %s", err)
			}
			println(string(data))
		}
	} else if CLI.Update.ProgressMode == "fancy" {
		var stopProgressFunc func()
		progressFunc, stopProgressFunc = makeFancyProgressFunc(CLI.Update.Product, absInstallDir)
		defer stopProgressFunc()
	} else {
		progressFunc = plainProgress
	}

	config := patcher.PatcherConfig{
		BaseUrl:         baseUrl,
		InstallDir:      absInstallDir,
		Product:         CLI.Update.Product,
		VerifyWorkers:   CLI.Update.VerifyWorkers,
		DownloadWorkers: CLI.Update.DownloadWorkers,
		ApplyWorkers:    CLI.Update.ApplyWorkers,
		XDeltaBinPath:   CLI.Update.XDeltaPath,
		DownloadConfig: patcher.DownloadConfig{
			MaxAttempts:              CLI.Update.DownloadMaxAttempts,
			RetryBaseDelay:           CLI.Update.DownloadBaseDelay,
			RetryWaitIncrementFactor: CLI.Update.DownloadDelayFactor,
			DownloadSpeedWindow:      CLI.Update.DownloadSpeedWindow,
			DownloadRequestTimeout:   CLI.Update.DownloadRequestTimemout,
			DownloadStallTimeout:     CLI.Update.DownloadStallTimeout,
		},
		ProgressInterval: time.Duration(CLI.Update.ProgressInterval) * time.Second,
		ProgressFunc:     progressFunc,
	}

	ctx, stopNotify := signal.NotifyContext(ctx, os.Interrupt)
	defer stopNotify()

	var instructionsData []byte
	if CLI.Update.Instructions == "-" {
		// Instructions from stdin.
		instructionsData, err = io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Couldn't read instructions.json from stdin: %s", err)
		}
	} else {
		instructionsData, err = os.ReadFile(CLI.Update.Instructions)
		if err != nil {
			log.Fatalf("Couldn't read instructions.json file '%s': %s", CLI.Update.Instructions, err)
		}
	}
	instructions, err := patcher.DecodeInstructions(instructionsData, CLI.Update.InstructionsHash)
	if err != nil {
		log.Fatalf("Couldn't decode instructions.json file '%s': %s", CLI.Update.Instructions, err)
	}

	err = patcher.RunPatcher(ctx, instructions, config)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Patcher process failed: %s", err)
	}

	if CLI.Update.ProgressMode == "fancy" {
		// Bit of a hack. The progress bar lib updates on a timer and if we exit straight after the final
		// progress update that update didn't have time to propagate yet.
		time.Sleep(time.Second)
	}
}

func main() {
	kongCtx := kong.Parse(&CLI)
	switch kongCtx.Command() {
	case "update <product> <base-url> <install-dir>":
		Update()
	case "about":
		printAbout()
	default:
		kongCtx.Fatalf("Unknown command %s", kongCtx.Command())
	}
}
