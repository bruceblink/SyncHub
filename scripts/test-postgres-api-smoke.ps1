param(
    [string]$ProjectRoot = "",
    [int]$Port = 18768,
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

if ([string]::IsNullOrWhiteSpace($DatabaseURL)) {
    $DatabaseURL = $env:DATABASE_URL
}
if ([string]::IsNullOrWhiteSpace($DatabaseURL)) {
    throw "DatabaseURL is required; pass -DatabaseURL or set DATABASE_URL"
}

$createdSchema = $false
$schema = $DatabaseSchema.Trim()
$schemaTool = Join-Path $ProjectRoot "scripts/postgres-schema.go"
if ([string]::IsNullOrWhiteSpace($schema)) {
    $schema = "synchub_smoke_$([System.Guid]::NewGuid().ToString('N'))"
    Push-Location $ProjectRoot
    try {
        & go run $schemaTool -database-url $DatabaseURL -schema $schema -action create
        if ($LASTEXITCODE -ne 0) {
            throw "create postgres smoke schema failed with exit code $LASTEXITCODE"
        }
        $createdSchema = $true
    }
    finally {
        Pop-Location
    }
}

try {
    & (Join-Path $ProjectRoot "scripts/test-local-api-smoke.ps1") `
        -ProjectRoot $ProjectRoot `
        -Port $Port `
        -DatabaseDriver postgres `
        -DatabaseURL $DatabaseURL `
        -DatabaseSchema $schema
    if ($LASTEXITCODE -ne 0) {
        throw "postgres api smoke failed with exit code $LASTEXITCODE"
    }
    Write-Output "postgres api smoke test passed"
}
finally {
    if ($createdSchema) {
        Push-Location $ProjectRoot
        try {
            & go run $schemaTool -database-url $DatabaseURL -schema $schema -action drop
            if ($LASTEXITCODE -ne 0) {
                Write-Warning "drop postgres smoke schema failed with exit code $LASTEXITCODE"
            }
        }
        finally {
            Pop-Location
        }
    }
}
