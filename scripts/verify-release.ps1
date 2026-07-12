param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$ProjectRoot = "",
    [string]$ReleaseDir = "",
    [string[]]$Targets = @("linux/amd64", "linux/arm64", "windows/amd64")
)

$ErrorActionPreference = "Stop"

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

function Assert-PathExists {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        throw "$Message path=[$Path]"
    }
}

function Assert-Equal {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Actual,
        [Parameter(Mandatory = $true)]
        [object]$Expected,
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    if ($Actual -ne $Expected) {
        throw "$Message actual=[$Actual] expected=[$Expected]"
    }
}

function Invoke-CheckedOutput {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [string[]]$Arguments = @()
    )

    $output = & $FilePath @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath $($Arguments -join ' ') failed with exit code $LASTEXITCODE`n$($output -join "`n")"
    }
    return [string]($output -join "`n")
}

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [string[]]$Arguments = @()
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

function Get-HostGOOS {
    if ($PSVersionTable.PSEdition -ne "Core" -or $IsWindows) {
        return "windows"
    }
    if ($IsLinux) {
        return "linux"
    }
    if ($IsMacOS) {
        return "darwin"
    }
    return ""
}

function Get-HostGOARCH {
    $arch = [string]$env:PROCESSOR_ARCHITECTURE
    if ([string]::IsNullOrWhiteSpace($arch)) {
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString()
    }
    switch -Regex ($arch.ToUpperInvariant()) {
        "AMD64|X86_64|X64" { return "amd64" }
        "ARM64|AARCH64" { return "arm64" }
        default { return "" }
    }
}

$Version = $Version.Trim()
if ([string]::IsNullOrWhiteSpace($Version)) {
    throw "version is required"
}

$scriptRoot = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($scriptRoot)) {
    $scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
}
if ([string]::IsNullOrWhiteSpace($ProjectRoot)) {
    $ProjectRoot = Join-Path $scriptRoot ".."
}
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).ProviderPath

if ([string]::IsNullOrWhiteSpace($ReleaseDir)) {
    $ReleaseDir = Join-Path (Join-Path $ProjectRoot "dist") "synchub-$Version"
}
elseif (-not [System.IO.Path]::IsPathRooted($ReleaseDir)) {
    $ReleaseDir = Join-Path $ProjectRoot $ReleaseDir
}
$ReleaseDir = (Resolve-Path -LiteralPath $ReleaseDir).ProviderPath
$archiveTool = Join-Path (Join-Path $ProjectRoot "scripts") "release-targz.go"
$unixExecutableFiles = @("synchub-api")
$deploymentFiles = @("docker-compose.release.yml", "fly.toml")

$checksumPath = Join-Path $ReleaseDir "SHA256SUMS.txt"
Assert-PathExists -Path $checksumPath -Message "checksum file is missing"

$checksums = @{}
foreach ($line in Get-Content -LiteralPath $checksumPath) {
    if ([string]::IsNullOrWhiteSpace($line)) {
        continue
    }
    if ($line -notmatch '^(?<hash>[0-9a-fA-F]{64})\s+\*?(?<name>[^/\\]+)$') {
        throw "invalid checksum line: $line"
    }
    $name = $Matches.name.Trim()
    if ($checksums.ContainsKey($name)) {
        throw "duplicate checksum entry: $name"
    }
    $checksums[$name] = $Matches.hash.ToLowerInvariant()
}

foreach ($deploymentFile in $deploymentFiles) {
    $deploymentPath = Join-Path $ReleaseDir $deploymentFile
    Assert-PathExists -Path $deploymentPath -Message "deployment file is missing"
    if (-not $checksums.ContainsKey($deploymentFile)) {
        throw "checksum entry is missing: $deploymentFile"
    }
    $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $deploymentPath).Hash.ToLowerInvariant()
    Assert-Equal -Actual $actualHash -Expected $checksums[$deploymentFile] -Message "checksum mismatch for $deploymentFile"
}

$hostTarget = "$(Get-HostGOOS)/$(Get-HostGOARCH)"
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) "synchub-release-verify-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $tempRoot | Out-Null
try {
    foreach ($target in $Targets) {
        Assert-Target -Target $target
        $goos, $goarch = $target -split "/"
        $artifactName = "synchub-$Version-$goos-$goarch"
        $archiveName = Get-ArchiveName -ArtifactName $artifactName -GOOS $goos
        $archivePath = Join-Path $ReleaseDir $archiveName
        Assert-PathExists -Path $archivePath -Message "release archive is missing"

        if (-not $checksums.ContainsKey($archiveName)) {
            throw "checksum entry is missing: $archiveName"
        }
        $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        Assert-Equal -Actual $actualHash -Expected $checksums[$archiveName] -Message "checksum mismatch for $archiveName"

        $extractDir = Join-Path $tempRoot $artifactName
        New-Item -ItemType Directory -Path $extractDir | Out-Null
        if ($archiveName.EndsWith(".zip")) {
            Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir
        }
        else {
            Invoke-HostGo -Arguments @(
                "run",
                $archiveTool,
                "-mode", "extract",
                "-source", $extractDir,
                "-archive", $archivePath,
                "-exec", ($unixExecutableFiles -join ",")
            )
        }

        Assert-PathExists -Path (Join-Path $extractDir "README.md") -Message "$archiveName missing README.md"
        Assert-PathExists -Path (Join-Path $extractDir "README.en.md") -Message "$archiveName missing README.en.md"
        Assert-PathExists -Path (Join-Path $extractDir "LICENSE") -Message "$archiveName missing LICENSE"

        $suffix = ""
        if ($goos -eq "windows") {
            $suffix = ".exe"
        }
        foreach ($binary in @("synchub-api")) {
            Assert-PathExists -Path (Join-Path $extractDir "$binary$suffix") -Message "$archiveName missing $binary$suffix"
        }

    }
}
finally {
    if (Test-Path -LiteralPath $tempRoot) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}

Write-Output "release artifacts verified: $ReleaseDir"
