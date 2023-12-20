# TotemArts CLI patcher

This is a patcher for TotemArts games that runs in the CLI.

## Basic usage

First download an xdelta binary from somewhere like `https://github.com/jmacd/xdelta-gpl/releases/tag/v3.0.11`

On Linux (untested) just install it from the package manager.

Build the patcher with `go build ./cmd/tapatcher`

Run it with `tapatcher.exe update <game_tag> <install_dir> -X <xdelta_path> -L <log_path>`
where game_tag is the tag of the game you want to install in the products.json, install_dir is where you want
to install the game, xdelta_path is the location of xdelta (if 'xdelta3' is in the PATH you can omit this)
and log_file is where you want to store logs (default fancy mode doesn't print logs).

E.g. `.\tapatcher.exe update renegade_x renx -X .\xdelta3-3.1.0-x86_64.exe -L tapatcher.log`

An alternative products URL (e.g. for Firestorm) can be specified with `-U <products_url>` (e.g.
`-U https://launcher.totemarts.services/products.json`)

You can add `--verbose` to make the logs more spammy. See `.\tapatcher.exe update --help` for more command
line arguments.

## High level approach

The CLI launcher uses the same three stage approach as the Vue/electron launcher. In the verify phase it
determines the checksums of existing files. In the download phase it downloads patches. In the apply phase
it applies those patches to get new files and moves those into place.

One major improvement compared to the Vue/electron launcher is the use of a manifest. A manifest file
(`ta-manifest.json`) is created in the install dir after the first successful update. This file contains the
product/game name (tag in the products.json) and for installed files the last modification time and the
last measured checksum (SHA256).

In the verify phase the last modification time of files on the filesystem is first compared against the
manifest, if it matches the file is considered to have the checksum written in the manifest. The practical
result is that the verify phase is almost instant now instead of taking a lot of time (on HDD) reading
20 or so GB on every verify even if nothing changed.

The download phase is slightly intelligent as well. If a patch file already exists from a previous failed
invocation (those files only get deleted upon successful completion) it's measured and if the checksum is
what's expected the download of that patch is skipped. This is not perfect, a file that was 99% downloaded
would still be redownloaded completely instead of only the remainder, but it should help.

## Progress modes

By default the patcher uses fancy progress mode, i.e. progress bars. The downside of this is that if stdout
(progress) and stderr (where logs go by default) both go to the terminal the logs mess up the progress bars.
Hence the fancy progress mode disables logs. If you want to you can force the "natural" behaviour of logging
to stderr by passing `-L -` ('-' means stderr in this case).

There's also a less fancy progress mode that's useful which can be activated with `--progress-mode plain`.
With this mode progress messages on stdout are simple lines, about 1 per second (speed can be set with
`--progress-interval`, default is equivalent to `--progress-interval 1`).

There's a third progress mode that outputs progress (but not logs) to JSON (`--progress-mode json`), one
JSON object per line. This is useful for calling the CLI patcher from a different process.

## From-instructions subcommand

The CLI patcher can be passed the contents of an instructions.json file directly, instead of having it go
through the product.json and release.json file. This is mostly useful for processes calling the CLI patcher.
To use this call `tapatcher.exe <product> <install_dir> <base_url>`. The new base_url argument is the "directory"
on the server containing the instructions.json file, the patcher needs to know this because the patches are
located in `<base_url>/full` and `<base_url>/delta` on the server. The contents of instructions.json should be
passed on stdin or by passing `-I <path_to_instructions_file>`.

## Development notes

During development replace `tapatcher.exe` with `go run ./cmd/tapatcher.exe` (from the root of the repo).

Before releasing run release.ps1 to update the licenses docs (used in the `about` subcommand).
