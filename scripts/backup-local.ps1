param(
    [string]$DataDir = ".data",
    [string]$OutputDir = ".backups"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $DataDir -PathType Container)) {
    throw "data directory not found: $DataDir"
}

$resolvedDataDir = Resolve-Path -LiteralPath $DataDir
$databasePath = Join-Path $resolvedDataDir "synchub.db"
$storagePath = Join-Path $resolvedDataDir "storage"

if (-not (Test-Path -LiteralPath $databasePath -PathType Leaf)) {
    throw "SQLite fallback database not found: $databasePath"
}

if (-not (Test-Path -LiteralPath $storagePath -PathType Container)) {
    throw "storage directory not found: $storagePath"
}

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$backupPath = Join-Path $OutputDir "synchub-local-$timestamp.zip"
$backupPath = [System.IO.Path]::GetFullPath($backupPath)

$stagingDir = Join-Path ([System.IO.Path]::GetTempPath()) "synchub-backup-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $stagingDir | Out-Null

try {
    Copy-Item -LiteralPath $databasePath -Destination (Join-Path $stagingDir "synchub.db")
    Copy-Item -LiteralPath $storagePath -Destination (Join-Path $stagingDir "storage") -Recurse
    $items = Get-ChildItem -LiteralPath $stagingDir
    Compress-Archive -LiteralPath $items.FullName -DestinationPath $backupPath -Force
    Write-Output $backupPath
}
finally {
    if (Test-Path -LiteralPath $stagingDir) {
        Remove-Item -LiteralPath $stagingDir -Recurse -Force
    }
}
