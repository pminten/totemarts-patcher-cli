# Steps to build a release ready file.
[CmdletBinding()]
param(
    [Parameter(HelpMessage="Version string to include, overrides the version from CHANGELOG.")]
    [string] $Version = "",

    # `go install github.com/google/go-licenses@latest` if you want to install manually.
    [Parameter(HelpMessage="Path to go-licenses (if not supplied will use version in PATH)")]
    [string] $GoLicensesPath = "go-licenses"
)

if ($Version -eq "") {
    # It's a little brittle but as long as the changelog format is kept it should work.
    $Version = (Get-Content CHANGELOG.md | select-string '## \[(\d+\.\d+\.\d+)\]')[0].Matches.Groups[1].Value
}

&"$GoLicensesPath" report ./cmd/tapatcher/ ./lib/patcher/ --template ./misc/license.tmpl > cmd/tapatcher/files/LICENSES.md
if ($LASTEXITCODE -ne 0) {
    throw "go-licenses failed"
}

go build -ldflags "-X main.Version=$Version" ./cmd/tapatcher
if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
}
