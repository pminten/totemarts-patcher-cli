package main

import (
	_ "embed"
	"fmt"
	"os"
	"runtime/debug"
)

// Use go-licenses (see release.ps1) to update this file after changing go.mod
//
//go:embed files/LICENSES.md
var licensesText string

func printAbout() {
	printVersion()
	println("")
	println("Licenses of used software follow.")
	println("")
	println(licensesText)
}

func printVersion() {
	buildTime := "<unknown build time>"
	buildRevision := "<unknown build revision>"
	// There should be a variable for this in debug/buildinfo but I couldn't get it to work.
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		panic("Can't read build info.")
	}
	for _, s := range buildInfo.Settings {
		if s.Key == "vcs.time" {
			buildTime = s.Value
		}
		if s.Key == "vcs.revision" {
			buildRevision = s.Value
		}
	}
	fmt.Fprintf(os.Stderr, "TotemArts CLI Patcher tool %s (%s, %s)\n", Version, buildTime, buildRevision)
}
