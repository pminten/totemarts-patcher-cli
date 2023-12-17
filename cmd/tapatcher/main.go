package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/alecthomas/kong"
	"github.com/pminten/totemarts-patcher-cli/lib/patcher"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var CLI struct {
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

		DownloadMaxAttempts int     `name:"download-max-attempts" default:"5" help:"How many times to try to download a file."`
		DownloadBaseDelay   float64 `name:"download-base-delay" default:"1" help:"How many seconds to wait between download retries at first."`
		DownloadDelayFactor float64 `name:"download-delay-factor" default:"1.5" help:"How much to multiply delay between download retries after each retry."`
		DownloadSpeedWindow int     `name:"download-speed-window" default:"5" help:"How many seconds to average download speed over."`

		ProgressInterval int    `name:"progress-interval" default:"1" help:"How often to report progress."`
		ProgressMode     string `name:"progress-mode" enum:"plain,fancy,json" default:"fancy" help:"How to report progress (plain, fancy or json)."`

		LogPretty bool `name:"log-pretty" help:"Use pretty logging."`
	} `cmd:"" help:"Install or update a game."`
}

func Update(kongCtx *kong.Context) {
	baseUrl, err := url.Parse(CLI.Update.BaseUrl)
	kongCtx.FatalIfErrorf(err, "Base-url is not a valid URL.")

	progressFunc := func(p patcher.Progress) {
		fmt.Printf("%#v\n", p)
	}

	config := patcher.PatcherConfig{
		BaseUrl:         baseUrl,
		InstallDir:      CLI.Update.InstallDir,
		Product:         CLI.Update.Product,
		VerifyWorkers:   CLI.Update.VerifyWorkers,
		DownloadWorkers: CLI.Update.DownloadWorkers,
		ApplyWorkers:    CLI.Update.ApplyWorkers,
		XDeltaBinPath:   CLI.Update.XDeltaPath,
		DownloadConfig: patcher.DownloadConfig{
			MaxAttempts:              CLI.Update.DownloadMaxAttempts,
			RetryBaseDelay:           time.Duration(CLI.Update.DownloadBaseDelay * float64(time.Second)),
			RetryWaitIncrementFactor: CLI.Update.DownloadDelayFactor,
			DownloadSpeedWindow:      CLI.Update.DownloadSpeedWindow,
		},
		ProgressInterval: time.Duration(CLI.Update.ProgressInterval) * time.Second,
		ProgressFunc:     progressFunc,
	}

	ctx := context.Background()
	ctx, stopNotify := signal.NotifyContext(ctx, os.Interrupt)
	defer stopNotify()

	if CLI.Update.LogPretty {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	var instructionsData []byte
	if CLI.Update.Instructions == "-" {
		// Instructions from stdin.
		instructionsData, err = io.ReadAll(os.Stdin)
		kongCtx.FatalIfErrorf(err, "Couldn't read instructions.json from stdin.", CLI.Update.Instructions)
	} else {
		instructionsData, err = os.ReadFile(CLI.Update.Instructions)
		kongCtx.FatalIfErrorf(err, "Couldn't read instructions.json file %q.", CLI.Update.Instructions)
	}
	instructions, err := patcher.DecodeInstructions(instructionsData, CLI.Update.InstructionsHash)
	kongCtx.FatalIfErrorf(err, "Couldn't decode instructions.json file %q.", CLI.Update.Instructions)

	err = patcher.RunPatcher(ctx, instructions, config)
	if err != nil && !errors.Is(err, context.Canceled) {
		kongCtx.Fatalf("%s", err)
	}
}

func main() {
	kongCtx := kong.Parse(&CLI)
	switch kongCtx.Command() {
	case "update <product> <base-url> <install-dir>":
		Update(kongCtx)
	default:
		kongCtx.Fatalf("Unknown command %s", kongCtx.Command())
	}
}
