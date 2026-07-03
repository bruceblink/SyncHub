param(
    [Parameter(Mandatory = $true)]
    [string]$BackupPath,
    [string]$DataDir = ".data",
    [switch]$Force
)

$ErrorActionPreference = "Stop"

function Resolve-FullPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    return [System.IO.Path]::GetFullPath($Path)
}

function Assert-SafeTargetDirectory {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $fullPath = Resolve-FullPath $Path
    $rootPath = [System.IO.Path]::GetPathRoot($fullPath)
    $trimmedPath = $fullPath.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
    $trimmedRoot = $rootPath.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
    $currentDir = (Get-Location).ProviderPath.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)

    if ([string]::IsNullOrWhiteSpace($trimmedPath) -or $trimmedPath -eq $trimmedRoot) {
        throw "refusing to restore into a filesystem root: $fullPath"
    }
    if ($trimmedPath -eq $currentDir) {
        throw "refusing to restore into the current working directory: $fullPath"
    }

    return $fullPath
}

if (-not (Test-Path -LiteralPath $BackupPath -PathType Leaf)) {
    throw "backup file not found: $BackupPath"
}

$resolvedDataDir = Assert-SafeTargetDirectory $DataDir

if ((Test-Path -LiteralPath $resolvedDataDir) -and -not $Force) {
    throw "data directory already exists: $resolvedDataDir; pass -Force to replace it"
}

$extractDir = Join-Path ([System.IO.Path]::GetTempPath()) "synchub-restore-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $extractDir | Out-Null

try {
    Expand-Archive -LiteralPath $BackupPath -DestinationPath $extractDir -Force
    $databasePath = Join-Path $extractDir "synchub.db"
    $storagePath = Join-Path $extractDir "storage"

    if (-not (Test-Path -LiteralPath $databasePath -PathType Leaf)) {
        throw "backup is missing synchub.db"
    }
    if (-not (Test-Path -LiteralPath $storagePath -PathType Container)) {
        throw "backup is missing storage directory"
    }

    if (Test-Path -LiteralPath $resolvedDataDir) {
        Remove-Item -LiteralPath $resolvedDataDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $resolvedDataDir | Out-Null
    Copy-Item -LiteralPath $databasePath -Destination (Join-Path $resolvedDataDir "synchub.db")
    Copy-Item -LiteralPath $storagePath -Destination (Join-Path $resolvedDataDir "storage") -Recurse
    Write-Output $resolvedDataDir
}
finally {
    if (Test-Path -LiteralPath $extractDir) {
        Remove-Item -LiteralPath $extractDir -Recurse -Force
    }
}
