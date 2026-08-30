# Tergum build script for Windows (PowerShell)
# Usage: .\build.ps1 [-Prod] [linux|darwin|windows|all]

param(
    [string]$Target = "all",
    [switch]$Prod
)

# Version info
if ($Prod) {
    $dirty = git status --porcelain 2>$null
    if ($dirty) {
        Write-Warning "Working tree is dirty. Prod build will use clean version anyway."
    }
}

# Read version from release file (single source of truth)
$ReleaseVersion = $null
if (Test-Path "release") {
    $ReleaseVersion = (Get-Content "release" -Raw).Trim()
}

if (Test-Path "version.json") {
    $versionJson = Get-Content "version.json" -Raw | ConvertFrom-Json
    $JsonVersion = $versionJson.version
    $Build = $versionJson.build
}

if ($ReleaseVersion) { $Version = $ReleaseVersion }
elseif ($JsonVersion) { $Version = $JsonVersion }
else { $Version = "3.0.0" }
if (-not $Build) { $Build = "dev" }

$Commit = git rev-parse --short HEAD 2>$null
if (-not $Commit) { $Commit = "none" }

$BuildDate = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")
$LdFlags = "-s -w -X `"github.com/gcclinux/tergum/internal/version.Version=$Version`" -X `"github.com/gcclinux/tergum/internal/version.Build=$Build`" -X `"github.com/gcclinux/tergum/internal/version.Commit=$Commit`" -X `"github.com/gcclinux/tergum/internal/version.BuildDate=$BuildDate`" -X `"github.com/gcclinux/tergum/cmd.Version=$Version`" -X `"github.com/gcclinux/tergum/cmd.Commit=$Commit`" -X `"github.com/gcclinux/tergum/cmd.BuildDate=$BuildDate`""

$OutputDir = "dist"
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

function Build-Linux {
    Write-Host "Building for Linux (amd64 & aarch64)..."
    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -ldflags="$LdFlags" -o "$OutputDir\tergum-amd64-linux" ./
    Write-Host "  -> $OutputDir\tergum-amd64-linux"
    $env:GOARCH = "arm64"
    go build -ldflags="$LdFlags" -o "$OutputDir\tergum-aarch64-linux" ./
    Write-Host "  -> $OutputDir\tergum-aarch64-linux"
}

function Build-Darwin {
    Write-Host "Building for macOS (arm64)..."
    $env:CGO_ENABLED = "0"
    $env:GOOS = "darwin"
    $env:GOARCH = "arm64"
    go build -ldflags="$LdFlags" -o "$OutputDir\tergum-arm64-macos" ./
    Write-Host "  -> $OutputDir\tergum-arm64-macos"
}

function Build-Windows {
    Write-Host "Building for Windows (amd64)..."
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    go build -ldflags="$LdFlags" -o "$OutputDir\tergum-amd64.exe" ./
    Write-Host "  -> $OutputDir\tergum-amd64.exe"
}

switch ($Target.ToLower()) {
    "linux"   { Build-Linux }
    "darwin"  { Build-Darwin }
    "macos"   { Build-Darwin }
    "windows" { Build-Windows }
    "win"     { Build-Windows }
    "all"     { Build-Linux; Build-Darwin; Build-Windows }
    default {
        Write-Host "Usage: .\build.ps1 [linux|darwin|windows|all]"
        Write-Host ""
        Write-Host "Options:"
        Write-Host "  linux    Build for Linux (amd64)"
        Write-Host "  darwin   Build for macOS (arm64)"
        Write-Host "  windows  Build for Windows (amd64)"
        Write-Host "  all      Build all platforms (default)"
        exit 1
    }
}

Write-Host ""
Write-Host "Build complete! (version: $Version, commit: $Commit)"
