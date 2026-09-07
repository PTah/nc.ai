$repo = $PSScriptRoot

# Make sure Go and Wails CLI are visible in this shell.
$goBin = Join-Path $env:ProgramFiles 'Go\bin'
$gopathBin = Join-Path $env:USERPROFILE 'go\bin'
$env:Path = "$goBin;$gopathBin;$env:Path"

Push-Location $repo
try {
    wails build -platform windows/amd64
    if ($LASTEXITCODE -ne 0) {
        Write-Error "wails build failed"
        exit 1
    }
}
finally {
    Pop-Location
}

$exe = Join-Path $repo 'build\bin\NotCursor.exe'
Write-Output "Build finished: $exe"
