param(
    [string]$ProjectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).ProviderPath,
    [int]$Port = 18765
)

$ErrorActionPreference = "Stop"

function Remove-DirectoryWithRetry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    for ($attempt = 1; $attempt -le 10; $attempt++) {
        try {
            if (Test-Path -LiteralPath $Path) {
                Remove-Item -LiteralPath $Path -Recurse -Force
            }
            return
        }
        catch {
            if ($attempt -eq 10) {
                throw
            }
            Start-Sleep -Milliseconds 200
        }
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

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) "synchub-api-smoke-$([System.Guid]::NewGuid().ToString('N'))"
$apiOut = Join-Path $tempRoot "api.out.log"
$apiErr = Join-Path $tempRoot "api.err.log"
$serverURL = "http://127.0.0.1:$Port"
$apiProcess = $null
$isWindows = $PSVersionTable.PSEdition -ne "Core" -or $IsWindows
$apiBinaryName = if ($isWindows) { "synchub-api.exe" } else { "synchub-api" }
$cliBinaryName = if ($isWindows) { "synchub-cli.exe" } else { "synchub-cli" }
$apiBinary = Join-Path $tempRoot $apiBinaryName
$cliBinary = Join-Path $tempRoot $cliBinaryName

try {
    New-Item -ItemType Directory -Path $tempRoot | Out-Null

    Invoke-Checked -FilePath "go" -Arguments @("build", "-o", $apiBinary, "./cmd/synchub-api") | Out-Null
    Invoke-Checked -FilePath "go" -Arguments @("build", "-o", $cliBinary, "./cmd/synchub-cli") | Out-Null

    $env:HTTP_ADDR = "127.0.0.1:$Port"
    $env:DATABASE_DRIVER = "sqlite"
    $env:DATABASE_URL = Join-Path $tempRoot "synchub.db"
    $env:LOCAL_STORAGE_ROOT = Join-Path $tempRoot "storage"
    $env:JWT_SECRET = "local-smoke-secret"

    $apiProcess = Start-Process -FilePath $apiBinary `
        -WorkingDirectory $ProjectRoot `
        -WindowStyle Hidden `
        -PassThru `
        -RedirectStandardOutput $apiOut `
        -RedirectStandardError $apiErr

    Invoke-Checked -FilePath $cliBinary -Arguments @("server", "wait", "--server", $serverURL, "--timeout", "20s", "--interval", "250ms") | Out-Null
    Invoke-Checked -FilePath $cliBinary -Arguments @("server", "status", "--server", $serverURL) | Out-Null
    $metrics = Invoke-Checked -FilePath $cliBinary -Arguments @("server", "metrics", "--server", $serverURL)
    Assert-OutputContains -Output $metrics -Expected "synchub_http_requests_total" -Message "metrics output missing request counter"

    $workspaceRoot = Join-Path $tempRoot "workspace"
    New-Item -ItemType Directory -Path $workspaceRoot | Out-Null
    $loginConfig = Join-Path $tempRoot "login.json"
    $email = "smoke-$([System.Guid]::NewGuid().ToString('N'))@example.com"
    $password = "password123"
    $historyPath = Join-Path $workspaceRoot "history.txt"
    $restoredPath = Join-Path $tempRoot "restored-history.txt"

    Invoke-Checked -FilePath $cliBinary -Arguments @("register", "--server", $serverURL, "--email", $email, "--password", $password, "--config", $loginConfig) | Out-Null
    Invoke-Checked -FilePath $cliBinary -Arguments @("workspace", "init", "--path", $workspaceRoot, "--remote-path", "/workspace", "--config", $loginConfig) | Out-Null

    [System.IO.File]::WriteAllText($historyPath, "version one")
    Invoke-Checked -FilePath $cliBinary -Arguments @("sync", "once", "--path", $workspaceRoot, "--config", $loginConfig, "--device-name", "smoke-device", "--platform", "smoke") | Out-Null

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
        [Console]::Error.WriteLine((Get-Content -LiteralPath $apiErr -Raw))
    }
    throw $caught
}
finally {
    if ($apiProcess -and -not $apiProcess.HasExited) {
        Stop-Process -Id $apiProcess.Id -Force -ErrorAction SilentlyContinue
        $apiProcess.WaitForExit()
    }
    if (Test-Path -LiteralPath $tempRoot) {
        Remove-DirectoryWithRetry -Path $tempRoot
    }
}
