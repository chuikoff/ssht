# SSHT Windows installer. Re-run to update. Does not touch ~/.ssh/config.
#   irm https://raw.githubusercontent.com/chuikoff/ssht/main/install/windows.ps1 | iex
#   .\install\windows.ps1 -LocalBinary .\ssht.exe
#   .\install\windows.ps1 -Force -Version v1.3.9

param(
    [string]$InstallDir = "$env:LOCALAPPDATA\ssht",
    [switch]$Force,
    [string]$LocalBinary = "",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$null = cmd /c "chcp 65001 >nul"

$Repo = "chuikoff/ssht"

function Write-Info([string]$Message) { Write-Host $Message -ForegroundColor Green }
function Write-Warn([string]$Message) { Write-Host $Message -ForegroundColor Yellow }

function Get-GoArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    switch ($arch.ToUpperInvariant()) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { throw "Архитектура '$arch' не поддерживается. Есть только windows/amd64 и windows/arm64." }
    }
}

function Install-Binary([string]$Source, [string]$Target) {
    $dir = Split-Path -Parent $Target
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
    if (Test-Path $Target) {
        $old = "$Target.old"
        Remove-Item $old -Force -ErrorAction SilentlyContinue
        try {
            Rename-Item -Path $Target -NewName (Split-Path -Leaf $old) -ErrorAction Stop
        } catch {
            throw "Не удалось заменить $Target. Закройте SSHT, иконку в трее и 'ssht daemon', затем запустите установщик снова."
        }
        try {
            Copy-Item -Path $Source -Destination $Target -Force
        } catch {
            Rename-Item -Path $old -NewName (Split-Path -Leaf $Target) -ErrorAction SilentlyContinue
            throw
        }
        Remove-Item $old -Force -ErrorAction SilentlyContinue
        return
    }
    Copy-Item -Path $Source -Destination $Target -Force
}

function Add-ToUserPath([string]$Dir) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $key = $Dir.Trim().TrimEnd('\').ToLowerInvariant()
    $parts = @()
    if ($userPath) {
        $parts = @($userPath -split ';' | Where-Object {
            $_ -and ($_.Trim().TrimEnd('\').ToLowerInvariant() -ne $key)
        })
    }
    $newPath = (@($Dir) + $parts) -join ';'
    if ($newPath -ne $userPath) {
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        Write-Info "Каталог добавлен в начало пользовательского PATH: $Dir"
        Write-Warn "Откройте новое окно терминала, чтобы команда ssht подхватилась."
    }
    $inSession = $false
    foreach ($p in ($env:Path -split ';')) {
        if ($p -and ($p.Trim().TrimEnd('\').ToLowerInvariant() -eq $key)) { $inSession = $true }
    }
    if (-not $inSession) { $env:Path = "$Dir;$env:Path" }
}

Write-Info "Установка SSHT"
Write-Info ""

if (-not $Force) {
    $existing = Get-Command ssht -ErrorAction SilentlyContinue
    if ($existing) {
        $current = "неизвестна"
        try {
            $verOut = & $existing.Source --version 2>&1 | Out-String
            if ($verOut -match 'v\d+\.\d+\.\d+') { $current = $Matches[0] }
        } catch { }
        Write-Warn "SSHT уже есть в PATH ($current): $($existing.Source)"
        $answer = Read-Host "Продолжить установку? (y/N)"
        if ($answer -notin @("y", "Y", "yes", "д", "Д", "да")) {
            Write-Info "Установка отменена."
            return
        }
    }
}

$targetPath = Join-Path $InstallDir "ssht.exe"

if ($LocalBinary -ne "") {
    if (-not (Test-Path -LiteralPath $LocalBinary)) {
        throw "Файл не найден: $LocalBinary"
    }
    Write-Info "Локальный файл: $LocalBinary"
    Install-Binary -Source $LocalBinary -Target $targetPath
} else {
    $arch = Get-GoArch
    Write-Info "Платформа: Windows ($arch)"

    $headers = @{
        "User-Agent" = "ssht-installer"
        "Accept"     = "application/vnd.github+json"
    }

    if ($Version) {
        $tag = $Version
        if ($tag -notmatch '^v') { $tag = "v$tag" }
    } else {
        Write-Info "Запрос последнего релиза..."
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $headers
        $tag = $release.tag_name
        if (-not $tag) { throw "Не удалось получить номер релиза с GitHub." }
    }
    Write-Info "Версия: $tag"

    $fileName = "ssht_${tag}_windows_${arch}.zip"
    $baseUrl = "https://github.com/$Repo/releases/download/$tag"
    $tempRoot = Join-Path $env:TEMP ("ssht-install-" + [guid]::NewGuid().ToString("n"))
    New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null
    try {
        $zipPath = Join-Path $tempRoot $fileName
        Write-Info "Скачивание $fileName..."
        try {
            Invoke-WebRequest -Uri "$baseUrl/$fileName" -OutFile $zipPath -UseBasicParsing -Headers @{ "User-Agent" = "ssht-installer" }
        } catch {
            throw "Не удалось скачать $fileName. Проверьте, что релиз $tag опубликован."
        }

        $sumPath = Join-Path $tempRoot "checksums.txt"
        Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $sumPath -UseBasicParsing -Headers @{ "User-Agent" = "ssht-installer" }
        $sumLine = Get-Content -LiteralPath $sumPath | Where-Object { $_ -like "*$fileName" } | Select-Object -First 1
        if (-not $sumLine) { throw "В checksums.txt нет записи для $fileName" }
        $expected = ($sumLine -split '\s+', 2)[0].ToLowerInvariant()
        $actual = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { throw "SHA256 не совпала для $fileName" }

        $extract = Join-Path $tempRoot "extract"
        Expand-Archive -LiteralPath $zipPath -DestinationPath $extract -Force
        $exe = Get-ChildItem -LiteralPath $extract -Recurse -Filter ssht.exe | Select-Object -First 1
        if (-not $exe) { throw "В архиве нет ssht.exe" }
        Install-Binary -Source $exe.FullName -Target $targetPath
    } finally {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Add-ToUserPath -Dir $InstallDir

Write-Info ""
Write-Info "SSHT установлен: $targetPath"
& $targetPath --version
