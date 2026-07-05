param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$ProjectRoot = "",
    [string]$OutputDir = "",
    [string[]]$Targets = @("linux/amd64", "linux/arm64", "windows/amd64")
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

function Invoke-HostGo {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $savedGOOS = $env:GOOS
    $savedGOARCH = $env:GOARCH
    $savedCGO = $env:CGO_ENABLED
    try {
        [Environment]::SetEnvironmentVariable("GOOS", $null, "Process")
        [Environment]::SetEnvironmentVariable("GOARCH", $null, "Process")
        [Environment]::SetEnvironmentVariable("CGO_ENABLED", $null, "Process")
        Invoke-Checked -FilePath "go" -Arguments $Arguments
    }
    finally {
        [Environment]::SetEnvironmentVariable("GOOS", $savedGOOS, "Process")
        [Environment]::SetEnvironmentVariable("GOARCH", $savedGOARCH, "Process")
        [Environment]::SetEnvironmentVariable("CGO_ENABLED", $savedCGO, "Process")
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

function Get-ArchiveName {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ArtifactName,
        [Parameter(Mandatory = $true)]
        [string]$GOOS
    )

    if ($GOOS -eq "windows") {
        return "$ArtifactName.zip"
    }
    return "$ArtifactName.tar.gz"
}

function Write-ReleaseArchive {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SourceDir,
        [Parameter(Mandatory = $true)]
        [string]$ArchivePath,
        [Parameter(Mandatory = $true)]
        [string]$GOOS,
        [Parameter(Mandatory = $true)]
        [string[]]$ExecutableFiles,
        [Parameter(Mandatory = $true)]
        [string]$ArchiveTool
    )

    if ($GOOS -eq "windows") {
        Compress-Archive -Path (Join-Path $SourceDir "*") -DestinationPath $ArchivePath
        return
    }

    Invoke-HostGo -Arguments @(
        "run",
        $ArchiveTool,
        "-mode", "create",
        "-source", $SourceDir,
        "-archive", $ArchivePath,
        "-exec", ($ExecutableFiles -join ",")
    )
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
$archiveTool = Join-Path (Join-Path $ProjectRoot "scripts") "release-targz.go"
$deploymentFiles = @("docker-compose.release.yml", "fly.toml")
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

        $archiveName = Get-ArchiveName -ArtifactName $artifactName -GOOS $goos
        $archivePath = Join-Path $releaseRoot $archiveName
        Write-ReleaseArchive -SourceDir $staging -ArchivePath $archivePath -GOOS $goos -ExecutableFiles ($binaries | ForEach-Object { $_.Name }) -ArchiveTool $archiveTool
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        $hashLines.Add("$hash  $archiveName")
        Remove-Item -LiteralPath $staging -Recurse -Force
    }

    foreach ($deploymentFile in $deploymentFiles) {
        $deploymentPath = Join-Path $ProjectRoot $deploymentFile
        if (-not (Test-Path -LiteralPath $deploymentPath)) {
            throw "deployment file is missing: $deploymentPath"
        }
        $deploymentDestination = Join-Path $releaseRoot $deploymentFile
        Copy-Item -LiteralPath $deploymentPath -Destination $deploymentDestination
        $deploymentHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $deploymentDestination).Hash.ToLowerInvariant()
        $hashLines.Add("$deploymentHash  $deploymentFile")
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
