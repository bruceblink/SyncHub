param(
    [string]$ProjectRoot = "",
    [int]$Port = 18765,
    [switch]$SkipLocalApiSmoke
)

$ErrorActionPreference = "Stop"

function Invoke-ExternalStep {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [string[]]$Arguments = @()
    )

    Write-Host "==> $Name"
    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

function Invoke-ScriptStep {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [scriptblock]$Script
    )

    Write-Host "==> $Name"
    & $Script
}

$scriptRoot = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($scriptRoot)) {
    $scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
}
if ([string]::IsNullOrWhiteSpace($ProjectRoot)) {
    $ProjectRoot = Join-Path $scriptRoot ".."
}
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).ProviderPath

Push-Location $ProjectRoot
try {
    Invoke-ExternalStep -Name "go fmt ./..." -FilePath "go" -Arguments @("fmt", "./...")
    Invoke-ExternalStep -Name "go vet ./..." -FilePath "go" -Arguments @("vet", "./...")
    Invoke-ExternalStep -Name "go test ./..." -FilePath "go" -Arguments @("test", "./...")

    if (-not $SkipLocalApiSmoke) {
        Invoke-ScriptStep -Name "local API smoke" -Script {
            & (Join-Path $ProjectRoot "scripts/test-local-api-smoke.ps1") -ProjectRoot $ProjectRoot -Port $Port
        }
    }
    Invoke-ScriptStep -Name "local backup restore smoke" -Script {
        & (Join-Path $ProjectRoot "scripts/test-local-backup-restore.ps1") -ProjectRoot $ProjectRoot
    }

    Write-Output "MVP checks passed"
}
finally {
    Pop-Location
}
