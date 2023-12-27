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

// Current version number. Gets injected via Github action.
var Version string = "<unknown version>"

type CommonUpdateOpts struct {
	VerifyWorkers   int    `name:"verify-workers" default:"4" help:"Number of concurrent file verifications."`
	DownloadWorkers int    `name:"download-workers" default:"4" help:"Number of concurrent patch downloads."`
	ApplyWorkers    int    `name:"apply-workers" default:"4" help:"Number of concurrent patching processes."`
	XDeltaPath      string `name:"xdelta" short:"X" default:"xdelta3" help:"Path to xdelta3 binary. If no directory name will also look for this in PATH."`

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
	LogFile       string `name:"log-file" short:"L" type:"path" help:"Where to store logs. Particularly useful with fancy progress mode as that hides logs, use '-' for stderr."`
}

var CLI struct {
	Update struct {
		Product    string `arg:"" name:"product" help:"Code of the game."`
		InstallDir string `arg:"" name:"install-dir" help:"Directory where the game should be."`

		ProductsUrl string `name:"products-url" short:"U" default:"https://launcher.totemarts.services/products.json" help:"Location of the products.json file."`

		CommonUpdateOpts
	} `cmd:"" help:"Install or update a game."`
	UpdateFromInstructions struct {
		Product    string `arg:"" name:"product" help:"Code of the game."`
		InstallDir string `arg:"" name:"install-dir" help:"Directory where the game should be."`
		BaseUrl    string `arg:"" name:"base-url" help:"URL of \"directory\" containing the instructions.json file."`

		Instructions string `name:"instructions" short:"I" default:"-" type:"existingfile" help:"Path of instructions.json file, use '-' for reading from stdin."`

		CommonUpdateOpts
	} `cmd:"" help:"Install or update a game using an already downloaded instructions.json file."`
	About struct {
	} `cmd:"" help:"Show license info."`
	Version struct {
	} `cmd:"" help:"Show version of patcher."`
}

func update() {
	product := CLI.Update.Product
	installDir := CLI.Update.InstallDir
	productsUrlStr := CLI.Update.ProductsUrl

	// Yes, this looks silly. It's the least ugly approach I've found for sharing arguments between
	// some but not all of the subcommands.
	commonOpts := CommonUpdateOpts{
		VerifyWorkers:   CLI.Update.VerifyWorkers,
		DownloadWorkers: CLI.Update.DownloadWorkers,
		ApplyWorkers:    CLI.Update.ApplyWorkers,
		XDeltaPath:      CLI.Update.XDeltaPath,

		DownloadMaxAttempts:     CLI.Update.DownloadMaxAttempts,
		DownloadBaseDelay:       CLI.Update.DownloadBaseDelay,
		DownloadDelayFactor:     CLI.Update.DownloadDelayFactor,
		DownloadSpeedWindow:     CLI.Update.DownloadSpeedWindow,
		DownloadRequestTimemout: CLI.Update.DownloadRequestTimemout,
		DownloadStallTimeout:    CLI.Update.DownloadStallTimeout,

		ProgressInterval: CLI.Update.ProgressInterval,
		ProgressMode:     CLI.Update.ProgressMode,

		Verbose:       CLI.Update.Verbose,
		OmitTimestamp: CLI.Update.OmitTimestamp,
		LogFile:       CLI.Update.LogFile,
	}

	setupLogging(&commonOpts)

	productsUrl, err := url.Parse(productsUrlStr)
	if err != nil {
		log.Fatalf("products-url is not a valid URL: %s", err)
	}

	resolved, err := patcher.ResolveInstructions(productsUrl, product)
	if err != nil {
		log.Fatalf("failed to resolve instructions.json: %s", err)
	}

	doUpdate(&commonOpts, product, installDir, resolved.BaseUrl, resolved.Instructions, &resolved.VersionName)
}

func updateFromInstructions() {
	product := CLI.UpdateFromInstructions.Product
	installDir := CLI.UpdateFromInstructions.InstallDir
	baseUrlStr := CLI.UpdateFromInstructions.BaseUrl
	instructionsPath := CLI.UpdateFromInstructions.Instructions
	var gameVersion *string = nil

	commonOpts := CommonUpdateOpts{
		VerifyWorkers:   CLI.UpdateFromInstructions.VerifyWorkers,
		DownloadWorkers: CLI.UpdateFromInstructions.DownloadWorkers,
		ApplyWorkers:    CLI.UpdateFromInstructions.ApplyWorkers,
		XDeltaPath:      CLI.UpdateFromInstructions.XDeltaPath,

		DownloadMaxAttempts:     CLI.UpdateFromInstructions.DownloadMaxAttempts,
		DownloadBaseDelay:       CLI.UpdateFromInstructions.DownloadBaseDelay,
		DownloadDelayFactor:     CLI.UpdateFromInstructions.DownloadDelayFactor,
		DownloadSpeedWindow:     CLI.UpdateFromInstructions.DownloadSpeedWindow,
		DownloadRequestTimemout: CLI.UpdateFromInstructions.DownloadRequestTimemout,
		DownloadStallTimeout:    CLI.UpdateFromInstructions.DownloadStallTimeout,

		ProgressInterval: CLI.UpdateFromInstructions.ProgressInterval,
		ProgressMode:     CLI.UpdateFromInstructions.ProgressMode,

		Verbose:       CLI.UpdateFromInstructions.Verbose,
		OmitTimestamp: CLI.UpdateFromInstructions.OmitTimestamp,
		LogFile:       CLI.UpdateFromInstructions.LogFile,
	}

	setupLogging(&commonOpts)

	baseUrl, err := url.Parse(baseUrlStr)
	if err != nil {
		log.Fatalf("base-url is not a valid URL: %s", err)
	}

	var instructionsData []byte
	if instructionsPath == "-" {
		// Instructions from stdin.
		instructionsData, err = io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Couldn't read instructions.json from stdin: %s", err)
		}
	} else {
		instructionsData, err = os.ReadFile(instructionsPath)
		if err != nil {
			log.Fatalf("Couldn't read instructions.json file '%s': %s", instructionsPath, err)
		}
	}
	instructions, err := patcher.DecodeInstructions(instructionsData)
	if err != nil {
		log.Fatalf("Couldn't decode instructions.json file '%s': %s", instructionsPath, err)
	}

	doUpdate(&commonOpts, product, installDir, baseUrl, instructions, gameVersion)
}

func setupLogging(commonOpts *CommonUpdateOpts) {
	if commonOpts.OmitTimestamp {
		log.SetFlags(0)
	}
	if commonOpts.LogFile != "" && commonOpts.LogFile != "-" {
		// - is stderr, which is the default.
		logFile, err := os.Create(commonOpts.LogFile)
		if err != nil {
			panic(fmt.Sprintf("Couldn't open log file '%s': %v", commonOpts.LogFile, err))
		}
		log.SetOutput(logFile)
	}
	if commonOpts.ProgressMode == "fancy" && commonOpts.LogFile == "" {
		log.SetOutput(io.Discard) // Avoid messing with the terminal while progress is being printed.
	}

}

func doUpdate(
	commonOpts *CommonUpdateOpts,
	product string,
	installDir string,
	baseUrl *url.URL,
	instructions []patcher.Instruction,
	gameVersion *string,
) {
	ctx := patcher.SetVerbose(context.Background(), commonOpts.Verbose)

	absInstallDir, err := filepath.Abs(installDir)
	if err != nil {
		log.Fatalf("install-dir is not a valid directory name: %s", err)
	}

	var progressFunc func(patcher.Progress)
	if commonOpts.ProgressMode == "json" {
		progressFunc = func(p patcher.Progress) {
			data, err := json.Marshal(p)
			if err != nil {
				log.Fatalf("Failed to serialize progress structure: %s", err)
			}
			fmt.Printf("%s\n", data)
		}
	} else if commonOpts.ProgressMode == "fancy" {
		var stopProgressFunc func()
		progressFunc, stopProgressFunc = makeFancyProgressFunc(product, absInstallDir, gameVersion)
		defer stopProgressFunc()
	} else {
		progressFunc = plainProgress
	}

	config := patcher.PatcherConfig{
		BaseUrl:         baseUrl,
		InstallDir:      absInstallDir,
		Product:         product,
		VerifyWorkers:   commonOpts.VerifyWorkers,
		DownloadWorkers: commonOpts.DownloadWorkers,
		ApplyWorkers:    commonOpts.ApplyWorkers,
		XDeltaBinPath:   commonOpts.XDeltaPath,
		DownloadConfig: patcher.DownloadConfig{
			MaxAttempts:              commonOpts.DownloadMaxAttempts,
			RetryBaseDelay:           commonOpts.DownloadBaseDelay,
			RetryWaitIncrementFactor: commonOpts.DownloadDelayFactor,
			DownloadSpeedWindow:      commonOpts.DownloadSpeedWindow,
			DownloadRequestTimeout:   commonOpts.DownloadRequestTimemout,
			DownloadStallTimeout:     commonOpts.DownloadStallTimeout,
		},
		ProgressInterval: time.Duration(commonOpts.ProgressInterval) * time.Second,
		ProgressFunc:     progressFunc,
	}

	ctx, stopNotify := signal.NotifyContext(ctx, os.Interrupt)
	defer stopNotify()

	err = patcher.RunPatcher(ctx, instructions, config)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Patcher process failed: %s", err)
	}

	if commonOpts.ProgressMode == "fancy" {
		// Bit of a hack. The progress bar lib updates on a timer and if we exit straight after the final
		// progress update that update didn't have time to propagate yet.
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
		}
	}
}

func main() {
	kongCtx := kong.Parse(&CLI)
	switch kongCtx.Command() {
	case "update <product> <install-dir>":
		update()
	case "update-from-instructions <product> <install-dir> <base-url>":
		updateFromInstructions()
	case "about":
		printAbout()
	case "version":
		printVersion()
	default:
		kongCtx.Fatalf("Unknown command %s", kongCtx.Command())
	}
}
