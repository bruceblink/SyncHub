param(
    [string]$ProjectRoot = "",
    [switch]$SkipInstall
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
$webAdminRoot = Join-Path $ProjectRoot "web-admin"

Push-Location $webAdminRoot
try {
    if (-not $SkipInstall) {
        & pnpm install --frozen-lockfile
        if ($LASTEXITCODE -ne 0) {
            throw "pnpm install failed with exit code $LASTEXITCODE"
        }
    }
    & pnpm build
    if ($LASTEXITCODE -ne 0) {
        throw "pnpm build failed with exit code $LASTEXITCODE"
    }
}
finally {
    Pop-Location
}
