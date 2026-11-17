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

# The click updater starts Windows PowerShell 5.1 with PowerShell 7's module
# path, so Get-FileHash is hidden and the window used to close on that error.
$script:InstallFailed = $false
trap {
    if ($script:InstallFailed) { exit 1 }
    $script:InstallFailed = $true
    Write-Host ""
    Write-Host "Ошибка установки: $_" -ForegroundColor Red
    Read-Host "Enter — закрыть"
    exit 1
}

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
    $leaf = Split-Path -Leaf $Target
    if (Test-Path -LiteralPath $Target) {
        # A fixed ssht.exe.old stays locked by the previous process and the next rename fails.
        $oldLeaf = "{0}.{1}.old" -f $leaf, [guid]::NewGuid().ToString("n")
        $old = Join-Path $dir $oldLeaf
        try {
            Rename-Item -LiteralPath $Target -NewName $oldLeaf -ErrorAction Stop
        } catch {
            throw "Не удалось заменить $Target ($($_.Exception.Message)). Закройте SSHT, иконку в трее и 'ssht daemon', затем запустите установщик снова."
        }
        try {
            Copy-Item -LiteralPath $Source -Destination $Target -Force
        } catch {
            Rename-Item -LiteralPath $old -NewName $leaf -ErrorAction SilentlyContinue
            throw
        }
    } else {
        Copy-Item -LiteralPath $Source -Destination $Target -Force
    }
    Get-ChildItem -LiteralPath $dir -Force -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -like "$leaf.*.old" -or $_.Name -eq "$leaf.old" -or $_.Name -eq "$leaf.prev" } |
        ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }
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
        $sha = [System.Security.Cryptography.SHA256]::Create()
        $stream = [System.IO.File]::OpenRead($zipPath)
        try {
            $actual = ([BitConverter]::ToString($sha.ComputeHash($stream))).Replace('-', '').ToLowerInvariant()
        } finally {
            $stream.Dispose()
            $sha.Dispose()
        }
        if ($actual -ne $expected) { throw "SHA256 не совпала для $fileName" }

        $extract = Join-Path $tempRoot "extract"
        Expand-Archive -LiteralPath $zipPath -DestinationPath $extract -Force
        $exe = Get-ChildItem -LiteralPath $extract -Recurse -Filter ssht.exe | Select-Object -First 1
        if (-not $exe) { throw "В архиве нет ssht.exe" }
        Install-Binary -Source $exe.FullName -Target $targetPath
    } finally {
        # Remove-Item dies on 8.3 TEMP (C:\Users\7636~1) and skips PATH: PowerShell/PowerShell#21070
        try {
            if (Test-Path -LiteralPath $tempRoot) {
                [System.IO.Directory]::Delete($tempRoot, $true)
            }
        } catch {
            Write-Warn "Не удалось удалить временный каталог: $tempRoot"
        }
    }
}

Add-ToUserPath -Dir $InstallDir

Write-Info ""
Write-Info "SSHT установлен: $targetPath"
& $targetPath --version
