package main

import (
	"github.com/alecthomas/kong"
)

var CLI struct {
	Update struct {
		InstallPath string `arg:"" name:"path" help:"Directory where the game should be."`
	} `cmd:"" help:"Install or update a game."`
}

func Update(InstallPath string) {

}

func main() {
	ctx := kong.Parse(&CLI)
	switch ctx.Command() {
	case "update <path>":
		Update(CLI.Update.InstallPath)
	default:
		ctx.Fatalf("Unknown command %s", ctx.Command())
	}
}
