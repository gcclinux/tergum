# Bump version across all source files
# Usage: .\bump_version.ps1 <version>
# Example: .\bump_version.ps1 1.2.0

param(
    [Parameter(Mandatory=$true, Position=0)]
    [string]$NewVersion
)

# Validate version format (semver: major.minor.patch with optional pre-release)
if ($NewVersion -notmatch '^\d+\.\d+\.\d+(-[\w.]+)?$') {
    Write-Error "Invalid version format: '$NewVersion'. Expected format: X.Y.Z or X.Y.Z-pre"
    exit 1
}

$RootDir = $PSScriptRoot

# Files to update
$releaseFile = Join-Path $RootDir "release"
$rootVersionJson = Join-Path $RootDir "version.json"
$internalVersionJson = Join-Path $RootDir "internal\version\version.json"

# 1. Update release file
Set-Content -Path $releaseFile -Value $NewVersion -NoNewline
Write-Host "Updated: release -> $NewVersion"

# 2. Update root version.json
$jsonContent = @"
{
  "version": "$NewVersion",
  "build": "dev"
}

"@
Set-Content -Path $rootVersionJson -Value $jsonContent -NoNewline
Write-Host "Updated: version.json -> $NewVersion"

# 3. Update internal/version/version.json (must match root exactly)
Set-Content -Path $internalVersionJson -Value $jsonContent -NoNewline
Write-Host "Updated: internal/version/version.json -> $NewVersion"

Write-Host ""
Write-Host "Version bumped to $NewVersion across all files."
Write-Host "Run .\build.ps1 to build with the new version."
