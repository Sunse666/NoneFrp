# Cross-compile release binaries into dist/.
# Usage: ./build.ps1 -Version 0.1.0
param(
    [string]$Version = "0.1.0-dev"
)

$ErrorActionPreference = "Stop"
$ldflags = "-s -w -X main.version=$Version"

$targets = @(
    @{ os = "linux";   arch = "amd64"; out = "nonefrp-linux-amd64" }
    @{ os = "linux";   arch = "arm64"; out = "nonefrp-linux-arm64" }
    @{ os = "darwin";  arch = "arm64"; out = "nonefrp-darwin-arm64" }
)

New-Item -ItemType Directory -Force -Path dist | Out-Null

foreach ($t in $targets) {
    $env:CGO_ENABLED = "0"
    $env:GOOS = $t.os
    $env:GOARCH = $t.arch
    Write-Host "building dist/$($t.out)"
    go build -trimpath -ldflags $ldflags -o "dist/$($t.out)" ./cmd/nonefrp
    if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.out)" }
}

# Windows: two builds. The windowless one is what a host double clicks; the
# console one shows logs live while debugging. Both write nonefrp.log.
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
Write-Host "building dist/NoneFrp-windows-amd64.exe (windowless)"
go build -trimpath -ldflags "$ldflags -H=windowsgui" -o "dist/NoneFrp-windows-amd64.exe" ./cmd/nonefrp
if ($LASTEXITCODE -ne 0) { throw "build failed for the windowless binary" }
Write-Host "building dist/NoneFrp-windows-amd64-console.exe"
go build -trimpath -ldflags $ldflags -o "dist/NoneFrp-windows-amd64-console.exe" ./cmd/nonefrp
if ($LASTEXITCODE -ne 0) { throw "build failed for the console binary" }

# Assemble everything a Linux server needs into one uploadable folder.
$bundle = "dist/nonefrp-bundle"
if (Test-Path $bundle) { Remove-Item -Recurse -Force $bundle }
New-Item -ItemType Directory -Force -Path $bundle | Out-Null

Copy-Item "dist/nonefrp-linux-amd64" $bundle
foreach ($name in @("setup.sh", "install.sh", "preflight.sh", "nonefrp.service", "nginx-relay.conf", "QUICKSTART.md", "OPERATIONS.md", "HEADSCALE.md", "join-room.cmd")) {
    Copy-Item "deploy/$name" $bundle
}

Write-Host "bundle ready: $bundle"
Write-Host "done"
