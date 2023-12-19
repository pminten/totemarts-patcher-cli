# Steps to build a release ready file.
[CmdletBinding()]
param(
    # `go install github.com/google/go-licenses@latest` if you don't have it yet.
    [Parameter(HelpMessage="Path to go-licenses (if not supplied will look in PATH)")]
    [string] $GoLicensesPath = "go-licenses"
)

&"$GoLicensesPath" report .\cmd\tapatcher\ .\lib\patcher\ --template .\misc\license.tmpl > cmd/tapatcher/generated/LICENSES.md
go build ./cmd/tapatcher
