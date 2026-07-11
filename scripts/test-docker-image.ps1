param(
    [string]$ProjectRoot = "",
    [string]$Image = "synchub:smoke",
    [string]$Version = "0.0.1",
    [int]$Port = 18767,
    [string]$GoProxy = "https://goproxy.cn,direct",
    [string]$BuildNetwork = "host",
    [string]$ContainerName = "synchub-image-smoke",
    [string]$VolumeName = "synchub-image-smoke-data",
    [switch]$SkipBuild
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

function Invoke-Docker {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Invoke-DockerOutput {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $output = & docker @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE`n$($output -join "`n")"
    }
    return [string]($output -join "`n")
}

function Invoke-DockerBestEffort {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    try {
        $oldErrorActionPreference = $ErrorActionPreference
        $ErrorActionPreference = "Continue"
        & docker @Arguments *> $null
    }
    catch {
        # Best-effort cleanup only.
    }
    finally {
        $ErrorActionPreference = $oldErrorActionPreference
    }
}

function Wait-Ready {
    param(
        [Parameter(Mandatory = $true)]
        [string]$URL,
        [int]$TimeoutSeconds = 60
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

Push-Location $ProjectRoot
try {
    if (-not $SkipBuild) {
        $buildArgs = @(
            "build",
            "--pull=false",
            "--build-arg", "VERSION=$Version",
            "--build-arg", "GOPROXY=$GoProxy",
            "-t", $Image
        )
        if (-not [string]::IsNullOrWhiteSpace($env:GO_IMAGE)) {
            $buildArgs += @("--build-arg", "GO_IMAGE=$env:GO_IMAGE")
        }
        if (-not [string]::IsNullOrWhiteSpace($env:RUNTIME_IMAGE)) {
            $buildArgs += @("--build-arg", "RUNTIME_IMAGE=$env:RUNTIME_IMAGE")
        }
        if (-not [string]::IsNullOrWhiteSpace($BuildNetwork)) {
            $buildArgs += @("--network", $BuildNetwork)
        }
        $buildArgs += @(".")
        Invoke-Docker -Arguments $buildArgs
    }

    $imageInspect = Invoke-DockerOutput -Arguments @("image", "inspect", $Image) | ConvertFrom-Json
    $imageVersion = [string]$imageInspect[0].Config.Labels."org.opencontainers.image.version"
    if ($imageVersion.Trim() -ne $Version) {
        throw "image version label mismatch actual=[$($imageVersion.Trim())] expected=[$Version]"
    }
    Invoke-Docker -Arguments @("run", "--rm", "--entrypoint", "/bin/sh", $Image, "-c", "test ! -e /usr/local/bin/synchub-cli")

    Invoke-DockerBestEffort -Arguments @("rm", "-f", $ContainerName)
    Invoke-DockerBestEffort -Arguments @("volume", "rm", "-f", $VolumeName)

    $containerDatabaseURL = $env:DATABASE_URL
    if ([string]::IsNullOrWhiteSpace($containerDatabaseURL)) {
        throw "DATABASE_URL is required for the PostgreSQL image smoke test"
    }

    Invoke-Docker -Arguments @(
        "run",
        "-d",
        "--name", $ContainerName,
        "-p", "${Port}:8765",
        "-e", "APP_ENV=test",
        "-e", "JWT_SECRET=image-smoke-secret",
        "-e", "DATABASE_DRIVER=postgres",
        "-e", "DATABASE_URL=$containerDatabaseURL",
        "-v", "${VolumeName}:/data",
        $Image
    )

    $ready = Wait-Ready -URL "http://127.0.0.1:$Port/readyz"
    $versionResponse = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/version" -TimeoutSec 5
    if (-not $versionResponse.Content.Contains("""version"":""$Version""")) {
        throw "version endpoint mismatch: $($versionResponse.Content)"
    }

    Write-Output $ready
    Write-Output "docker image smoke test passed"
}
catch {
    $caught = $_
    try {
        & docker logs $ContainerName
    }
    catch {
        # Best-effort logs only.
    }
    throw $caught
}
finally {
    try {
        Invoke-DockerBestEffort -Arguments @("rm", "-f", $ContainerName)
        Invoke-DockerBestEffort -Arguments @("volume", "rm", "-f", $VolumeName)
    }
    finally {
        Pop-Location
    }
}
