# Cross-compile release binaries into dist/.
# Usage: ./build.ps1 -Version 0.1.0
param(
    [string]$Version = "0.1.0-dev"
)

$ErrorActionPreference = "Stop"
$ldflags = "-s -w -X main.version=$Version"

$targets = @(
    @{ os = "windows"; arch = "amd64"; out = "NoneFrp-windows-amd64.exe" }
    @{ os = "linux";   arch = "amd64"; out = "nonerp-linux-amd64" }
    @{ os = "linux";   arch = "arm64"; out = "nonerp-linux-arm64" }
    @{ os = "darwin";  arch = "arm64"; out = "nonerp-darwin-arm64" }
)

New-Item -ItemType Directory -Force -Path dist | Out-Null

foreach ($t in $targets) {
    $env:CGO_ENABLED = "0"
    $env:GOOS = $t.os
    $env:GOARCH = $t.arch
    Write-Host "building dist/$($t.out)"
    go build -trimpath -ldflags $ldflags -o "dist/$($t.out)" ./cmd/nonerp
    if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.out)" }
}

# Assemble everything a Linux server needs into one uploadable folder.
$bundle = "dist/nonerp-bundle"
if (Test-Path $bundle) { Remove-Item -Recurse -Force $bundle }
New-Item -ItemType Directory -Force -Path $bundle | Out-Null

Copy-Item "dist/nonerp-linux-amd64" $bundle
foreach ($name in @("setup.sh", "install.sh", "preflight.sh", "nonerp.service", "nginx-relay.conf", "QUICKSTART.md", "OPERATIONS.md")) {
    Copy-Item "deploy/$name" $bundle
}

Write-Host "bundle ready: $bundle"
Write-Host "done"
