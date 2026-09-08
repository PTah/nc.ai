# UTF-8 with BOM expected for Windows PowerShell 5.1
param(
    # Default: after a successful build, (re)start NotCursor.exe.
    # Pass -NoRestart to only build (rename trick still used if the exe is locked).
    [switch]$NoRestart,
    # Kept for compatibility; restart is already the default.
    [switch]$Restart
)

$ErrorActionPreference = 'Stop'

$utf8 = New-Object System.Text.UTF8Encoding $false
[Console]::OutputEncoding = $utf8
[Console]::InputEncoding = $utf8
$OutputEncoding = $utf8
$null = cmd /c 'chcp 65001 >NUL'

$repo = $PSScriptRoot
$exeDir = Join-Path $repo 'build\bin'
$exe = Join-Path $exeDir 'NotCursor.exe'
$old = Join-Path $exeDir 'NotCursor.exe.old'

# Make sure Go and Wails CLI are visible in this shell.
$goBin = Join-Path $env:ProgramFiles 'Go\bin'
$gopathBin = Join-Path $env:USERPROFILE 'go\bin'
$env:Path = "$goBin;$gopathBin;$env:Path"

function Get-NotCursorProcesses {
    Get-Process -ErrorAction SilentlyContinue |
        Where-Object {
            $_.ProcessName -eq 'NotCursor' -or
            ($_.Path -and ($_.Path -ieq $exe))
        }
}

$running = @(Get-NotCursorProcesses)
$isLocked = $running.Count -gt 0
# Default behaviour is restart; -NoRestart opts out. -Restart is a no-op alias.
$doRestart = -not $NoRestart
if ($Restart -and $NoRestart) {
    Write-Warning "Both -Restart and -NoRestart passed; -NoRestart wins."
}

if ($isLocked) {
    Write-Host "NotCursor is running (PID: $($running.Id -join ', '))."
    Write-Host "Windows cannot overwrite a locked .exe — renaming it to .old, then building a new one."
    if (Test-Path -LiteralPath $old) {
        Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $exe) {
        # Rename works even while the process is running; overwrite/delete does not.
        Rename-Item -LiteralPath $exe -NewName 'NotCursor.exe.old' -Force
    }
}

$buildOk = $false
Push-Location $repo
try {
    wails build -platform windows/amd64
    if ($LASTEXITCODE -ne 0) {
        throw "wails build failed (exit $LASTEXITCODE)"
    }
    if (-not (Test-Path -LiteralPath $exe)) {
        throw "Build reported success but missing: $exe"
    }
    $buildOk = $true
    Write-Host "Build finished: $exe"
}
catch {
    Write-Host "Build failed: $_" -ForegroundColor Red
    # Roll back rename so the still-running app keeps a discoverable path on disk.
    if ((Test-Path -LiteralPath $old) -and -not (Test-Path -LiteralPath $exe)) {
        Rename-Item -LiteralPath $old -NewName 'NotCursor.exe' -Force
        Write-Host "Restored NotCursor.exe from .old after failed build."
    }
    exit 1
}
finally {
    Pop-Location
}

if (-not $buildOk) { exit 1 }

if (-not $doRestart) {
    if (Test-Path -LiteralPath $old) {
        Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
    }
    Write-Host "Done (no restart)."
    exit 0
}

# Detached helper: stop old PIDs → start new exe → delete .old
# Must outlive this shell (which dies when NotCursor exits).
$pids = @($running | ForEach-Object { $_.Id })
$updater = Join-Path $env:TEMP ("nc-selfupdate-{0}.ps1" -f [guid]::NewGuid().ToString('N'))

$updaterBody = @'
param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [Parameter(Mandatory = $true)][string]$OldPath,
    [string]$TargetPids = ''
)
$ErrorActionPreference = 'SilentlyContinue'
$ids = @()
if ($TargetPids) {
    $ids = @(
        $TargetPids.Split(@(',', ';', ' '), [System.StringSplitOptions]::RemoveEmptyEntries) |
            ForEach-Object { [int]$_ }
    )
}
Start-Sleep -Milliseconds 400
foreach ($id in $ids) {
    if ($id -gt 0) { Stop-Process -Id $id -Force -ErrorAction SilentlyContinue }
}
$deadline = (Get-Date).AddSeconds(20)
while ((Get-Date) -lt $deadline) {
    $left = @()
    foreach ($id in $ids) {
        $left += @(Get-Process -Id $id -ErrorAction SilentlyContinue)
    }
    if ($left.Count -eq 0) { break }
    Start-Sleep -Milliseconds 200
}
Start-Sleep -Milliseconds 400
if (Test-Path -LiteralPath $ExePath) {
    Start-Process -FilePath $ExePath -WorkingDirectory (Split-Path -Parent $ExePath)
}
Start-Sleep -Seconds 2
if (Test-Path -LiteralPath $OldPath) {
    Remove-Item -LiteralPath $OldPath -Force -ErrorAction SilentlyContinue
}
Remove-Item -LiteralPath $MyInvocation.MyCommand.Path -Force -ErrorAction SilentlyContinue
'@

# PowerShell 5.1 sources: UTF-8 with BOM
$utf8Bom = New-Object System.Text.UTF8Encoding $true
[System.IO.File]::WriteAllText($updater, $updaterBody, $utf8Bom)

$psExe = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$pidArgs = ($pids | ForEach-Object { $_.ToString() }) -join ','
$argList = @(
    '-NoProfile'
    '-ExecutionPolicy', 'Bypass'
    '-File', $updater
    '-ExePath', $exe
    '-OldPath', $old
    '-TargetPids', $pidArgs
)

Write-Host "Self-update: starting helper (stop old process if any, then launch new exe)…"
Start-Process -FilePath $psExe -ArgumentList $argList -WindowStyle Hidden

# Give the helper a moment to spawn; if NotCursor was running, this shell exits with it.
Start-Sleep -Milliseconds 300
exit 0
