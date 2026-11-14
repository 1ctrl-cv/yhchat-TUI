# Build yhchat-tui for Windows and Linux.
#   pwsh -File scripts\build.ps1
# Outputs binaries into .\dist (override with -OutDir).

param(
    [string]$OutDir = (Join-Path $PSScriptRoot "..\dist")
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Push-Location $Root
try {
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

    $targets = @(
        @{ GOOS = "windows"; GOARCH = "amd64"; Name = "yhchat-tui-windows-amd64.exe" },
        @{ GOOS = "windows"; GOARCH = "arm64"; Name = "yhchat-tui-windows-arm64.exe" },
        @{ GOOS = "linux";   GOARCH = "amd64"; Name = "yhchat-tui-linux-amd64" },
        @{ GOOS = "linux";   GOARCH = "arm64"; Name = "yhchat-tui-linux-arm64" }
    )

    foreach ($t in $targets) {
        $env:GOOS = $t.GOOS
        $env:GOARCH = $t.GOARCH
        $env:CGO_ENABLED = "0"
        Write-Host "building $($t.GOOS)/$($t.GOARCH) -> $($t.Name)"
        go build -trimpath -ldflags "-s -w" -o (Join-Path $OutDir $t.Name) .
        if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.GOOS)/$($t.GOARCH)" }
    }

    Write-Host ""
    Get-ChildItem $OutDir | Select-Object Name, Length | Format-Table -AutoSize
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}
