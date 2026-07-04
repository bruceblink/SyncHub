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

    & go build -o $apiBinary ./cmd/synchub-api
    & go build -o $cliBinary ./cmd/synchub-cli

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

    & $cliBinary server wait --server $serverURL --timeout 20s --interval 250ms
    & $cliBinary server status --server $serverURL

    Write-Output "local api smoke test passed"
}
catch {
    if (Test-Path -LiteralPath $apiErr) {
        Write-Error (Get-Content -LiteralPath $apiErr -Raw)
    }
    throw
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
