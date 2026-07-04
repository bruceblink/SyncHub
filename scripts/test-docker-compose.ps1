param(
    [string]$ProjectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).ProviderPath,
    [int]$Port = 18766,
    [string]$Version = "0.0.1",
    [string]$GoProxy = "https://goproxy.cn,direct",
    [string]$ProjectName = "synchub-smoke"
)

$ErrorActionPreference = "Stop"

function Invoke-Compose {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    & docker compose -p $ProjectName @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Wait-Ready {
    param(
        [Parameter(Mandatory = $true)]
        [string]$URL,
        [int]$TimeoutSeconds = 45
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $URL -TimeoutSec 2
            if ($response.StatusCode -eq 200 -and $response.Content.Contains('"status":"ready"')) {
                return $response.Content
            }
        }
        catch {
            Start-Sleep -Seconds 2
        }
    }

    throw "SyncHub API did not become ready at $URL within ${TimeoutSeconds}s"
}

$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).ProviderPath
$env:SYNCHUB_VERSION = $Version
$env:GOPROXY = $GoProxy
$env:SYNCHUB_PORT = [string]$Port
$env:SYNCHUB_CONTAINER_NAME = "$ProjectName-api"

Push-Location $ProjectRoot
try {
    $existingContainer = & docker ps -aq --filter "name=^/$env:SYNCHUB_CONTAINER_NAME$"
    if ($LASTEXITCODE -ne 0) {
        throw "docker ps failed with exit code $LASTEXITCODE"
    }
    if ($existingContainer) {
        & docker rm -f $env:SYNCHUB_CONTAINER_NAME | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "docker rm -f $env:SYNCHUB_CONTAINER_NAME failed with exit code $LASTEXITCODE"
        }
    }
    Invoke-Compose -Arguments @("down", "--volumes", "--remove-orphans")
    Invoke-Compose -Arguments @("build", "synchub-api")
    Invoke-Compose -Arguments @("up", "-d", "synchub-api")
    $ready = Wait-Ready -URL "http://127.0.0.1:$Port/readyz"
    Write-Output $ready
    Write-Output "docker compose smoke test passed"
}
catch {
    $caught = $_
    try {
        & docker compose -p $ProjectName logs synchub-api
    }
    catch {
        # Best-effort logs only.
    }
    throw $caught
}
finally {
    try {
        Invoke-Compose -Arguments @("down", "--volumes", "--remove-orphans")
    }
    finally {
        Pop-Location
    }
}
