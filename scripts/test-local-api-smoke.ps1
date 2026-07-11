param(
    [string]$ProjectRoot = "",
    [int]$Port = 18765,
    [string]$DatabaseDriver = "",
    [string]$DatabaseURL = "",
    [string]$DatabaseSchema = ""
)

$ErrorActionPreference = "Stop"

$scriptRoot = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($scriptRoot)) {
    $scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
}
if ([string]::IsNullOrWhiteSpace($ProjectRoot)) {
    $ProjectRoot = Join-Path $scriptRoot ".."
}
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).ProviderPath

function Remove-DirectoryWithRetry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    for ($attempt = 1; $attempt -le 30; $attempt++) {
        try {
            if (Test-Path -LiteralPath $Path) {
                Remove-Item -LiteralPath $Path -Recurse -Force
            }
            return
        }
        catch {
            if ($attempt -eq 30) {
                throw
            }
            Start-Sleep -Milliseconds 500
        }
    }
}

function Write-GitHubAnnotation {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Level,
        [Parameter(Mandatory = $true)]
        [string]$Title,
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    if ($env:GITHUB_ACTIONS -ne "true") {
        return
    }
    $escapedTitle = $Title.Replace("%", "%25").Replace("`r", "%0D").Replace("`n", "%0A").Replace(":", "%3A").Replace(",", "%2C")
    $escapedMessage = $Message.Replace("%", "%25").Replace("`r", "%0D").Replace("`n", "%0A")
    Write-Output "::$Level title=$escapedTitle::$escapedMessage"
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

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $output = & $FilePath @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath $($Arguments -join ' ') failed with exit code $LASTEXITCODE`n$($output -join "`n")"
    }
    return $output
}

function Assert-OutputContains {
    param(
        [Parameter(Mandatory = $true)]
        [object[]]$Output,
        [Parameter(Mandatory = $true)]
        [string]$Expected,
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    $text = [string]($Output -join "`n")
    if (-not $text.Contains($Expected)) {
        throw "$Message missing=[$Expected] output=[$text]"
    }
}

function Assert-FileContent {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Expected,
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        throw "$Message path does not exist: $Path"
    }
    $actual = [System.IO.File]::ReadAllText($Path)
    if ($actual -ne $Expected) {
        throw "$Message actual=[$actual] expected=[$Expected]"
    }
}

function Assert-PathMissing {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    if (Test-Path -LiteralPath $Path) {
        throw "$Message path should not exist: $Path"
    }
}

function Get-EnvSnapshot {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Names
    )

    $snapshot = @{}
    foreach ($name in $Names) {
        $item = Get-Item -LiteralPath "Env:\$name" -ErrorAction SilentlyContinue
        if ($item) {
            $snapshot[$name] = @{ Exists = $true; Value = $item.Value }
        }
        else {
            $snapshot[$name] = @{ Exists = $false; Value = "" }
        }
    }
    return $snapshot
}

function Restore-EnvSnapshot {
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Snapshot
    )

    foreach ($name in $Snapshot.Keys) {
        if ($Snapshot[$name].Exists) {
            Set-Item -LiteralPath "Env:\$name" -Value $Snapshot[$name].Value
        }
        else {
            Remove-Item -LiteralPath "Env:\$name" -ErrorAction SilentlyContinue
        }
    }
}

function Set-OptionalEnv {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [string]$Value = ""
    )

    if ([string]::IsNullOrWhiteSpace($Value)) {
        Remove-Item -LiteralPath "Env:\$Name" -ErrorAction SilentlyContinue
    }
    else {
        Set-Item -LiteralPath "Env:\$Name" -Value $Value
    }
}

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) "synchub-api-smoke-$([System.Guid]::NewGuid().ToString('N'))"
$apiOut = Join-Path $tempRoot "api.out.log"
$apiErr = Join-Path $tempRoot "api.err.log"
$serverURL = "http://127.0.0.1:$Port"
$apiProcess = $null
$isWindows = $PSVersionTable.PSEdition -ne "Core" -or $IsWindows
$supportsStartWindowStyle = $isWindows -and $PSVersionTable.PSEdition -ne "Core"
$apiBinaryName = if ($isWindows) { "synchub-api.exe" } else { "synchub-api" }
$cliBinaryName = if ($isWindows) { "synchub-cli.exe" } else { "synchub-cli" }
$apiBinary = Join-Path $tempRoot $apiBinaryName
$cliBinary = Join-Path $tempRoot $cliBinaryName
$envSnapshot = Get-EnvSnapshot -Names @("APP_ENV", "HTTP_ADDR", "DATABASE_DRIVER", "DATABASE_URL", "DATABASE_SCHEMA", "LOCAL_STORAGE_ROOT", "JWT_SECRET")

try {
    New-Item -ItemType Directory -Path $tempRoot | Out-Null

    Invoke-Checked -FilePath "go" -Arguments @("build", "-o", $apiBinary, "./cmd/synchub-api") | Out-Null
    Invoke-Checked -FilePath "go" -Arguments @("build", "-o", $cliBinary, "./cmd/synchub-cli") | Out-Null

    $effectiveDatabaseURL = $DatabaseURL.Trim()
    $effectiveDatabaseDriver = $DatabaseDriver.Trim().ToLowerInvariant()
    if ([string]::IsNullOrWhiteSpace($effectiveDatabaseDriver)) {
        $lowerURL = $effectiveDatabaseURL.ToLowerInvariant()
        if ($lowerURL.StartsWith("postgres://") -or $lowerURL.StartsWith("postgresql://")) {
            $effectiveDatabaseDriver = "postgres"
        }
        else {
            $effectiveDatabaseDriver = "sqlite"
        }
    }
    if ($effectiveDatabaseDriver -eq "postgresql") {
        $effectiveDatabaseDriver = "postgres"
    }
    if ($effectiveDatabaseDriver -eq "sqlite" -and [string]::IsNullOrWhiteSpace($effectiveDatabaseURL)) {
        $effectiveDatabaseURL = Join-Path $tempRoot "synchub.db"
    }
    if ($effectiveDatabaseDriver -eq "postgres" -and [string]::IsNullOrWhiteSpace($effectiveDatabaseURL)) {
        throw "DatabaseURL is required when DatabaseDriver is postgres"
    }
    if ($effectiveDatabaseDriver -ne "sqlite" -and $effectiveDatabaseDriver -ne "postgres") {
        throw "unsupported smoke database driver: $effectiveDatabaseDriver"
    }

    $env:APP_ENV = "test"
    $env:HTTP_ADDR = "127.0.0.1:$Port"
    $env:DATABASE_DRIVER = $effectiveDatabaseDriver
    $env:DATABASE_URL = $effectiveDatabaseURL
    Set-OptionalEnv -Name "DATABASE_SCHEMA" -Value $DatabaseSchema
    $env:LOCAL_STORAGE_ROOT = Join-Path $tempRoot "storage"
    $env:JWT_SECRET = "local-smoke-secret"

    $startArgs = @{
        FilePath               = $apiBinary
        WorkingDirectory       = $ProjectRoot
        PassThru               = $true
        RedirectStandardOutput = $apiOut
        RedirectStandardError  = $apiErr
    }
    if ($supportsStartWindowStyle) {
        $startArgs.WindowStyle = "Hidden"
    }
    $apiProcess = Start-Process @startArgs

    Invoke-Checked -FilePath $cliBinary -Arguments @("server", "wait", "--server", $serverURL, "--timeout", "20s", "--interval", "250ms") | Out-Null
    Invoke-Checked -FilePath $cliBinary -Arguments @("server", "status", "--server", $serverURL) | Out-Null
    $metrics = Invoke-Checked -FilePath $cliBinary -Arguments @("server", "metrics", "--server", $serverURL)
    Assert-OutputContains -Output $metrics -Expected "synchub_http_requests_total" -Message "metrics output missing request counter"
    $openapi = Invoke-Checked -FilePath $cliBinary -Arguments @("server", "openapi", "--server", $serverURL)
    Assert-OutputContains -Output $openapi -Expected "openapi: 3.0.3" -Message "openapi output missing version"

    $workspaceRoot = Join-Path $tempRoot "workspace"
    $peerWorkspaceRoot = Join-Path $tempRoot "peer-workspace"
    New-Item -ItemType Directory -Path $workspaceRoot | Out-Null
    New-Item -ItemType Directory -Path $peerWorkspaceRoot | Out-Null
    $loginConfig = Join-Path $tempRoot "login.json"
    $email = "smoke-$([System.Guid]::NewGuid().ToString('N'))@example.com"
    $password = "password123"
    $historyPath = Join-Path $workspaceRoot "history.txt"
    $sharedPath = Join-Path $workspaceRoot "shared.txt"
    $peerSharedPath = Join-Path $peerWorkspaceRoot "shared.txt"
    $restoredPath = Join-Path $tempRoot "restored-history.txt"

    Invoke-Checked -FilePath $cliBinary -Arguments @("register", "--server", $serverURL, "--email", $email, "--password", $password, "--config", $loginConfig) | Out-Null
    Invoke-Checked -FilePath $cliBinary -Arguments @("workspace", "init", "--path", $workspaceRoot, "--remote-path", "/workspace", "--config", $loginConfig) | Out-Null

    [System.IO.File]::WriteAllText($historyPath, "version one")
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $workspaceRoot, "--config", $loginConfig, "--device-name", "smoke-device", "--platform", "smoke") | Out-Null

    Invoke-Checked -FilePath $cliBinary -Arguments @("workspace", "init", "--path", $peerWorkspaceRoot, "--remote-path", "/workspace", "--config", $loginConfig) | Out-Null
    [System.IO.File]::WriteAllText($sharedPath, "shared version one")
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $workspaceRoot, "--config", $loginConfig, "--device-name", "smoke-device", "--platform", "smoke") | Out-Null
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $peerWorkspaceRoot, "--config", $loginConfig, "--device-name", "smoke-peer", "--platform", "smoke") | Out-Null
    Assert-FileContent -Path $peerSharedPath -Expected "shared version one" -Message "peer workspace did not pull created file"

    [System.IO.File]::WriteAllText($sharedPath, "shared version two")
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $workspaceRoot, "--config", $loginConfig, "--device-name", "smoke-device", "--platform", "smoke") | Out-Null
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $peerWorkspaceRoot, "--config", $loginConfig, "--device-name", "smoke-peer", "--platform", "smoke") | Out-Null
    Assert-FileContent -Path $peerSharedPath -Expected "shared version two" -Message "peer workspace did not pull updated file"

    Remove-Item -LiteralPath $sharedPath
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $workspaceRoot, "--config", $loginConfig, "--device-name", "smoke-device", "--platform", "smoke") | Out-Null
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $peerWorkspaceRoot, "--config", $loginConfig, "--device-name", "smoke-peer", "--platform", "smoke") | Out-Null
    Assert-PathMissing -Path $peerSharedPath -Message "peer workspace did not apply remote delete"
    $trash = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "trash", "--path", $peerWorkspaceRoot)
    Assert-OutputContains -Output $trash -Expected "shared.txt" -Message "peer trash output missing deleted file"

    $daemonStatus = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--status")
    Assert-OutputContains -Output $daemonStatus -Expected "daemon: not run" -Message "daemon status should start as not run"

    $daemonPause = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--pause")
    Assert-OutputContains -Output $daemonPause -Expected "daemon paused:" -Message "daemon pause output missing"

    $pausedOnce = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--config", $loginConfig, "--once")
    Assert-OutputContains -Output $pausedOnce -Expected "sync skipped: daemon is paused" -Message "paused daemon should skip sync once"

    $pausedStatus = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--status")
    Assert-OutputContains -Output $pausedStatus -Expected "daemon: paused" -Message "daemon status should show paused"

    $daemonResume = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--resume")
    Assert-OutputContains -Output $daemonResume -Expected "daemon resumed:" -Message "daemon resume output missing"

    $daemonOnce = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--config", $loginConfig, "--once", "--device-name", "smoke-device", "--platform", "smoke")
    Assert-OutputContains -Output $daemonOnce -Expected "sync completed:" -Message "daemon sync once should complete after resume"

    $daemonNoWatch = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--config", $loginConfig, "--no-watch", "--cycles", "1", "--device-name", "smoke-device", "--platform", "smoke")
    Assert-OutputContains -Output $daemonNoWatch -Expected "daemon stopped: sync cycles reached 1" -Message "daemon no-watch smoke cycle should stop after one cycle"

    $daemonStatus = Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "daemon", "--path", $workspaceRoot, "--status")
    Assert-OutputContains -Output $daemonStatus -Expected "daemon: ok" -Message "daemon status should show ok after sync"

    [System.IO.File]::WriteAllText($historyPath, "version two")
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $workspaceRoot, "--config", $loginConfig, "--device-name", "smoke-device", "--platform", "smoke") | Out-Null

    $versions = Invoke-Checked -FilePath $cliBinary -Arguments @("file", "versions", "--path", $workspaceRoot, "--config", $loginConfig, "--remote-path", "/workspace/history.txt", "--limit", "5")
    Assert-OutputContains -Output $versions -Expected "versions: 2" -Message "version history count mismatch"
    Assert-OutputContains -Output $versions -Expected "v2 size=" -Message "version history missing v2"
    Assert-OutputContains -Output $versions -Expected "v1 size=" -Message "version history missing v1"

    $pin = Invoke-Checked -FilePath $cliBinary -Arguments @("file", "pin", "--path", $workspaceRoot, "--config", $loginConfig, "--remote-path", "/workspace/history.txt", "--version", "1")
    Assert-OutputContains -Output $pin -Expected "pinned:" -Message "pin output missing action"
    $pinText = [string]($pin -join "`n")
    if ($pinText.Contains("pinned at: -")) {
        throw "pin output did not include a pinned timestamp"
    }

    $unpin = Invoke-Checked -FilePath $cliBinary -Arguments @("file", "unpin", "--path", $workspaceRoot, "--config", $loginConfig, "--remote-path", "/workspace/history.txt", "--version", "1")
    Assert-OutputContains -Output $unpin -Expected "unpinned:" -Message "unpin output missing action"
    Assert-OutputContains -Output $unpin -Expected "pinned at: -" -Message "unpin output still has pinned timestamp"

    $restore = Invoke-Checked -FilePath $cliBinary -Arguments @("file", "restore", "--path", $workspaceRoot, "--config", $loginConfig, "--remote-path", "/workspace/history.txt", "--version", "1")
    Assert-OutputContains -Output $restore -Expected "restored: /workspace/history.txt" -Message "restore output missing restored path"
    Assert-OutputContains -Output $restore -Expected "version: 3" -Message "restore output missing new version"

    Invoke-Checked -FilePath $cliBinary -Arguments @("file", "download", "--path", $workspaceRoot, "--config", $loginConfig, "--remote-path", "/workspace/history.txt", "--output", $restoredPath) | Out-Null
    Assert-FileContent -Path $restoredPath -Expected "version one" -Message "restored version content mismatch"

    Write-Output "local api smoke test passed"
}
catch {
    $caught = $_
    if (Test-Path -LiteralPath $apiErr) {
        $apiErrText = Get-Content -LiteralPath $apiErr -Raw
        [Console]::Error.WriteLine($apiErrText)
        if (-not [string]::IsNullOrWhiteSpace($apiErrText)) {
            Write-GitHubAnnotation -Level "error" -Title "api stderr" -Message $apiErrText
        }
    }
    Write-GitHubAnnotation -Level "error" -Title "local api smoke failed" -Message $caught.Exception.Message
    throw $caught
}
finally {
    if ($apiProcess -and -not $apiProcess.HasExited) {
        Stop-Process -Id $apiProcess.Id -Force -ErrorAction SilentlyContinue
        $apiProcess.WaitForExit()
    }
    if (Test-Path -LiteralPath $tempRoot) {
        try {
            Remove-DirectoryWithRetry -Path $tempRoot
        }
        catch {
            $message = "temporary cleanup failed: $($_.Exception.Message)"
            Write-Warning $message
            Write-GitHubAnnotation -Level "warning" -Title "temporary cleanup failed" -Message $message
        }
    }
    Restore-EnvSnapshot -Snapshot $envSnapshot
}
