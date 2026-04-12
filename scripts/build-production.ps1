#Requires -Version 5.1

[CmdletBinding()]
param(
    [ValidateSet("windows-amd64", "linux-amd64")]
    [string]$Target = "linux-amd64",

    [string]$ApiBaseUrl = "__PANSHOW_SAME_ORIGIN__",

    [ValidateSet("auto", "ci", "install", "skip")]
    [string]$FrontendInstallMode = "auto",

    [switch]$SkipTests,

    [switch]$NoArchive
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Ensure-Utf8Console {
    $codePage = chcp.com
    Write-Host $codePage
    if ($codePage -notmatch "65001") {
        chcp.com 65001 | Out-Host
        $codePage = chcp.com
        if ($codePage -notmatch "65001") {
            throw "Console code page is not UTF-8 (65001)."
        }
    }
    [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
    $script:OutputEncoding = [System.Text.UTF8Encoding]::new($false)
}

function Invoke-Step {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,

        [Parameter(Mandatory = $true)]
        [scriptblock]$ScriptBlock
    )

    Write-Host "`n==> $Name"
    & $ScriptBlock
}

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [string[]]$ArgumentList = @()
    )

    & $FilePath @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath failed with exit code $LASTEXITCODE."
    }
}

function Assert-ChildPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Root,

        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [string]$Label
    )

    $resolvedRoot = [System.IO.Path]::GetFullPath($Root)
    $resolvedPath = [System.IO.Path]::GetFullPath($Path)
    if (-not $resolvedPath.StartsWith($resolvedRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to operate on $Label outside workspace: $resolvedPath"
    }
}

function Copy-DirectoryContents {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,

        [Parameter(Mandatory = $true)]
        [string]$Destination
    )

    if (-not (Test-Path -LiteralPath $Source -PathType Container)) {
        throw "Source directory does not exist: $Source"
    }
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    Copy-Item -Path (Join-Path $Source "*") -Destination $Destination -Recurse -Force
}

Ensure-Utf8Console

$root = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$frontendDir = Join-Path $root "frontend"
$frontendDist = Join-Path $frontendDir "dist"
$backendDir = Join-Path $root "backend"
$embedDist = Join-Path $backendDir "internal\web\dist"
$backendBin = Join-Path $backendDir "bin"
$releaseRoot = Join-Path $root "release"
$releaseDir = Join-Path $releaseRoot "panshow-$Target"
$archivePath = Join-Path $releaseRoot "panshow-$Target.zip"

Assert-ChildPath -Root $root -Path $embedDist -Label "embed dist"
Assert-ChildPath -Root $root -Path $releaseDir -Label "release directory"
Assert-ChildPath -Root $root -Path $archivePath -Label "release archive"

switch ($Target) {
    "windows-amd64" {
        $goos = "windows"
        $goarch = "amd64"
        $binaryName = "panshow-server.exe"
    }
    "linux-amd64" {
        $goos = "linux"
        $goarch = "amd64"
        $binaryName = "panshow-server"
    }
}

$binaryPath = Join-Path $backendBin $binaryName
$npmCommand = "npm"
$npmCmd = Get-Command "npm.cmd" -ErrorAction SilentlyContinue
if ($null -ne $npmCmd) {
    $npmCommand = $npmCmd.Source
}

Invoke-Step "Install frontend dependencies" {
    $installMode = $FrontendInstallMode
    if ($installMode -eq "auto") {
        if (Test-Path -LiteralPath (Join-Path $frontendDir "node_modules") -PathType Container) {
            $installMode = "install"
        } else {
            $installMode = "ci"
        }
    }

    if ($installMode -eq "skip") {
        Write-Host "Skipping frontend dependency install."
        return
    }

    Push-Location $frontendDir
    try {
        Write-Host "Running npm $installMode"
        Invoke-Native $npmCommand @($installMode)
    } finally {
        Pop-Location
    }
}

Invoke-Step "Build frontend" {
    Push-Location $frontendDir
    $previousApiBaseUrl = $env:VITE_API_BASE_URL
    try {
        $env:VITE_API_BASE_URL = if ([string]::IsNullOrWhiteSpace($ApiBaseUrl)) { "__PANSHOW_SAME_ORIGIN__" } else { $ApiBaseUrl }
        Invoke-Native $npmCommand @("run", "build")
    } finally {
        if ($null -eq $previousApiBaseUrl) {
            Remove-Item Env:\VITE_API_BASE_URL -ErrorAction SilentlyContinue
        } else {
            $env:VITE_API_BASE_URL = $previousApiBaseUrl
        }
        Pop-Location
    }
}

Invoke-Step "Copy frontend dist for Go embed" {
    if (-not (Test-Path -LiteralPath (Join-Path $frontendDist "index.html") -PathType Leaf)) {
        throw "frontend/dist/index.html was not generated."
    }
    New-Item -ItemType Directory -Force -Path $embedDist | Out-Null
    Get-ChildItem -LiteralPath $embedDist -Force |
        Where-Object { $_.Name -ne "placeholder.txt" } |
        Remove-Item -Recurse -Force
    Copy-DirectoryContents -Source $frontendDist -Destination $embedDist
}

if (-not $SkipTests) {
    Invoke-Step "Run backend tests" {
        Push-Location $backendDir
        try {
            Invoke-Native "go" @("test", "./...")
        } finally {
            Pop-Location
        }
    }
}

Invoke-Step "Build backend $Target" {
    New-Item -ItemType Directory -Force -Path $backendBin | Out-Null
    Push-Location $backendDir
    $previousCGO = $env:CGO_ENABLED
    $previousGOOS = $env:GOOS
    $previousGOARCH = $env:GOARCH
    try {
        $env:CGO_ENABLED = "0"
        $env:GOOS = $goos
        $env:GOARCH = $goarch
        Invoke-Native "go" @("build", "-trimpath", "-ldflags=-s -w", "-o", $binaryPath, ".\cmd\server")
    } finally {
        if ($null -eq $previousCGO) { Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $previousCGO }
        if ($null -eq $previousGOOS) { Remove-Item Env:\GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGOOS }
        if ($null -eq $previousGOARCH) { Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGOARCH }
        Pop-Location
    }
}

if (-not $NoArchive) {
    Invoke-Step "Package release archive" {
        if (Test-Path -LiteralPath $releaseDir) {
            Assert-ChildPath -Root $releaseRoot -Path $releaseDir -Label "release directory"
            Remove-Item -LiteralPath $releaseDir -Recurse -Force
        }
        New-Item -ItemType Directory -Force -Path (Join-Path $releaseDir "config") | Out-Null
        New-Item -ItemType Directory -Force -Path (Join-Path $releaseDir "logs") | Out-Null
        $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
        [System.IO.File]::WriteAllText((Join-Path $releaseDir "logs\.keep"), "", $utf8NoBom)
        Copy-Item -LiteralPath $binaryPath -Destination (Join-Path $releaseDir $binaryName) -Force
        Copy-Item -LiteralPath (Join-Path $backendDir ".env.example") -Destination (Join-Path $releaseDir "config\.env.example") -Force
        Copy-Item -LiteralPath (Join-Path $root "README.md") -Destination (Join-Path $releaseDir "README.md") -Force
        if (Test-Path -LiteralPath $archivePath) {
            Remove-Item -LiteralPath $archivePath -Force
        }
        Compress-Archive -Path (Join-Path $releaseDir "*") -DestinationPath $archivePath -Force
    }
}

Write-Host "`nProduction build complete."
Write-Host "Binary: $binaryPath"
if (-not $NoArchive) {
    Write-Host "Archive: $archivePath"
}
