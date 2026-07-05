param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$ProjectRoot = "",
    [string]$ReleaseDir = "",
    [string[]]$Targets = @("windows/amd64", "linux/amd64", "linux/arm64", "darwin/arm64")
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
    switch -Regex ($arch.ToUpperInvariant()) {
        "AMD64|X86_64" { return "amd64" }
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

$checksumPath = Join-Path $ReleaseDir "SHA256SUMS.txt"
Assert-PathExists -Path $checksumPath -Message "checksum file is missing"

$checksums = @{}
foreach ($line in Get-Content -LiteralPath $checksumPath) {
    if ([string]::IsNullOrWhiteSpace($line)) {
        continue
    }
    if ($line -notmatch '^(?<hash>[0-9a-fA-F]{64})\s+\*?(?<name>.+\.zip)$') {
        throw "invalid checksum line: $line"
    }
    $name = $Matches.name.Trim()
    if ($checksums.ContainsKey($name)) {
        throw "duplicate checksum entry: $name"
    }
    $checksums[$name] = $Matches.hash.ToLowerInvariant()
}

$hostTarget = "$(Get-HostGOOS)/$(Get-HostGOARCH)"
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) "synchub-release-verify-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $tempRoot | Out-Null
try {
    foreach ($target in $Targets) {
        Assert-Target -Target $target
        $goos, $goarch = $target -split "/"
        $artifactName = "synchub-$Version-$goos-$goarch"
        $archiveName = "$artifactName.zip"
        $archivePath = Join-Path $ReleaseDir $archiveName
        Assert-PathExists -Path $archivePath -Message "release archive is missing"

        if (-not $checksums.ContainsKey($archiveName)) {
            throw "checksum entry is missing: $archiveName"
        }
        $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        Assert-Equal -Actual $actualHash -Expected $checksums[$archiveName] -Message "checksum mismatch for $archiveName"

        $extractDir = Join-Path $tempRoot $artifactName
        Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir

        Assert-PathExists -Path (Join-Path $extractDir "README.md") -Message "$archiveName missing README.md"
        Assert-PathExists -Path (Join-Path $extractDir "LICENSE") -Message "$archiveName missing LICENSE"

        $suffix = ""
        if ($goos -eq "windows") {
            $suffix = ".exe"
        }
        foreach ($binary in @("synchub-api", "synchub-cli", "synchub-agent")) {
            Assert-PathExists -Path (Join-Path $extractDir "$binary$suffix") -Message "$archiveName missing $binary$suffix"
        }

        if ($target -eq $hostTarget) {
            $expectedVersion = "SyncHub $Version"
            $cliVersion = Invoke-CheckedOutput -FilePath (Join-Path $extractDir "synchub-cli$suffix") -Arguments @("version")
            Assert-Equal -Actual $cliVersion.Trim() -Expected $expectedVersion -Message "CLI version mismatch"

            $agentVersion = Invoke-CheckedOutput -FilePath (Join-Path $extractDir "synchub-agent$suffix") -Arguments @("--version")
            Assert-Equal -Actual $agentVersion.Trim() -Expected $expectedVersion -Message "agent version mismatch"
        }
    }
}
finally {
    if (Test-Path -LiteralPath $tempRoot) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}

Write-Output "release artifacts verified: $ReleaseDir"
