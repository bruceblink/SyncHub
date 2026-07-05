param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$ProjectRoot = "",
    [string]$OutputDir = "",
    [string[]]$Targets = @("windows/amd64", "linux/amd64", "linux/arm64", "darwin/arm64")
)

$ErrorActionPreference = "Stop"

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Assert-Target {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Target
    )

    $parts = $Target -split "/"
    if ($parts.Count -ne 2 -or [string]::IsNullOrWhiteSpace($parts[0]) -or [string]::IsNullOrWhiteSpace($parts[1])) {
        throw "target must use GOOS/GOARCH format: $Target"
    }
}

$Version = $Version.Trim()
if ([string]::IsNullOrWhiteSpace($Version)) {
    throw "version is required"
}
if ($Version -notmatch '^[0-9A-Za-z][0-9A-Za-z._+-]*$') {
    throw "version may only contain letters, numbers, dot, underscore, plus, and hyphen"
}

$scriptRoot = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($scriptRoot)) {
    $scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
}

if ([string]::IsNullOrWhiteSpace($ProjectRoot)) {
    $ProjectRoot = Join-Path $scriptRoot ".."
}
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).ProviderPath

if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $OutputDir = Join-Path $ProjectRoot "dist"
}
elseif (-not [System.IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir = Join-Path $ProjectRoot $OutputDir
}
if (-not (Test-Path -LiteralPath $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}
$OutputDir = (Resolve-Path -LiteralPath $OutputDir).ProviderPath
$releaseRoot = Join-Path $OutputDir "synchub-$Version"
if (Test-Path -LiteralPath $releaseRoot) {
    throw "release output already exists: $releaseRoot"
}
New-Item -ItemType Directory -Path $releaseRoot | Out-Null

$binaries = @(
    @{ Name = "synchub-api"; Package = "./cmd/synchub-api" },
    @{ Name = "synchub-cli"; Package = "./cmd/synchub-cli" },
    @{ Name = "synchub-agent"; Package = "./cmd/synchub-agent" }
)
$ldflags = "-s -w -X github.com/bruceblink/SyncHub/internal/version.Version=$Version"
$hashLines = New-Object System.Collections.Generic.List[string]

$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
Push-Location $ProjectRoot
try {
    foreach ($target in $Targets) {
        Assert-Target -Target $target
        $goos, $goarch = $target -split "/"
        $artifactName = "synchub-$Version-$goos-$goarch"
        $staging = Join-Path $releaseRoot $artifactName
        New-Item -ItemType Directory -Path $staging | Out-Null

        $env:GOOS = $goos
        $env:GOARCH = $goarch
        $env:CGO_ENABLED = "0"

        foreach ($binary in $binaries) {
            $binaryName = $binary.Name
            if ($goos -eq "windows") {
                $binaryName = "$binaryName.exe"
            }
            $outputPath = Join-Path $staging $binaryName
            Invoke-Checked -FilePath "go" -Arguments @(
                "build",
                "-trimpath",
                "-ldflags", $ldflags,
                "-o", $outputPath,
                $binary.Package
            )
        }

        Copy-Item -LiteralPath (Join-Path $ProjectRoot "README.md") -Destination $staging
        Copy-Item -LiteralPath (Join-Path $ProjectRoot "LICENSE") -Destination $staging

        $archivePath = Join-Path $releaseRoot "$artifactName.zip"
        Compress-Archive -Path (Join-Path $staging "*") -DestinationPath $archivePath
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        $hashLines.Add("$hash  $(Split-Path -Leaf $archivePath)")
        Remove-Item -LiteralPath $staging -Recurse -Force
    }

    $hashLines | Set-Content -LiteralPath (Join-Path $releaseRoot "SHA256SUMS.txt") -Encoding ascii
    Write-Output "release artifacts written: $releaseRoot"
}
finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
    Pop-Location
}
