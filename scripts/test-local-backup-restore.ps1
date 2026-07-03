param(
    [string]$ProjectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).ProviderPath
)

$ErrorActionPreference = "Stop"

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

function Join-ChildPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$BasePath,
        [Parameter(Mandatory = $true)]
        [string[]]$Children
    )

    $path = $BasePath
    foreach ($child in $Children) {
        $path = Join-Path $path $child
    }
    return $path
}

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) "synchub-backup-test-$([System.Guid]::NewGuid().ToString('N'))"
$dataDir = Join-Path $tempRoot "data"
$backupDir = Join-Path $tempRoot "backups"
$restoreDir = Join-Path $tempRoot "restored"

try {
    New-Item -ItemType Directory -Path (Join-ChildPath $dataDir "storage", "objects") -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $dataDir "synchub.db") -Value "sqlite-data" -NoNewline
    Set-Content -LiteralPath (Join-ChildPath $dataDir "storage", "objects", "file.txt") -Value "object-data" -NoNewline

    $backupPath = & (Join-ChildPath $ProjectRoot "scripts", "backup-local.ps1") -DataDir $dataDir -OutputDir $backupDir
    $backupPath = [string]$backupPath
    Assert-PathExists -Path $backupPath -Message "backup was not created"

    $expandedDir = Join-Path $tempRoot "expanded"
    Expand-Archive -LiteralPath $backupPath -DestinationPath $expandedDir
    $archiveEntries = (Get-ChildItem -LiteralPath $expandedDir -Recurse).FullName
    if (-not ($archiveEntries -match [regex]::Escape((Join-ChildPath "expanded" "synchub.db")))) {
        throw "backup archive missing synchub.db"
    }
    if (-not ($archiveEntries -match [regex]::Escape((Join-ChildPath "expanded" "storage", "objects", "file.txt")))) {
        throw "backup archive missing storage object"
    }

    $restoredPath = & (Join-ChildPath $ProjectRoot "scripts", "restore-local.ps1") -BackupPath $backupPath -DataDir $restoreDir
    $restoredPath = [string]$restoredPath
    Assert-Equal -Actual ([System.IO.Path]::GetFullPath($restoredPath)) -Expected ([System.IO.Path]::GetFullPath($restoreDir)) -Message "restore output path mismatch"
    Assert-Equal -Actual (Get-Content -LiteralPath (Join-Path $restoreDir "synchub.db") -Raw) -Expected "sqlite-data" -Message "restored database content mismatch"
    Assert-Equal -Actual (Get-Content -LiteralPath (Join-ChildPath $restoreDir "storage", "objects", "file.txt") -Raw) -Expected "object-data" -Message "restored storage content mismatch"

    $blocked = $false
    try {
        & (Join-ChildPath $ProjectRoot "scripts", "restore-local.ps1") -BackupPath $backupPath -DataDir $restoreDir | Out-Null
    }
    catch {
        if ($_.Exception.Message -like "*pass -Force to replace it*") {
            $blocked = $true
        }
        else {
            throw
        }
    }
    if (-not $blocked) {
        throw "restore should refuse to replace existing data without -Force"
    }

    Set-Content -LiteralPath (Join-Path $restoreDir "synchub.db") -Value "changed" -NoNewline
    & (Join-ChildPath $ProjectRoot "scripts", "restore-local.ps1") -BackupPath $backupPath -DataDir $restoreDir -Force | Out-Null
    Assert-Equal -Actual (Get-Content -LiteralPath (Join-Path $restoreDir "synchub.db") -Raw) -Expected "sqlite-data" -Message "force restore did not replace database"

    Write-Output "local backup restore test passed"
}
finally {
    if (Test-Path -LiteralPath $tempRoot) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}
