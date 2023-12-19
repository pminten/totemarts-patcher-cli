package main

import (
	_ "embed"
)

// Use go-licenses (see release.ps1) to update this file after changing go.mod
//
//go:embed generated/LICENSES.md
var licensesText string

func printAbout() {
	println("TotemArts CLI Patcher tool")
	println("")
	println("Licenses of used software follow.")
	println("")
	println(licensesText)
}
