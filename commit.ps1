param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$Message
)

$repo = $PSScriptRoot

git -C $repo add -A
if ($LASTEXITCODE -ne 0) {
    Write-Error "git add failed"
    exit 1
}

git -C $repo diff --cached --quiet
if ($LASTEXITCODE -eq 0) {
    Write-Output "Nothing to commit."
    exit 0
}

git -C $repo commit -m $Message
if ($LASTEXITCODE -ne 0) {
    Write-Error "git commit failed"
    exit 1
}

git -C $repo push
if ($LASTEXITCODE -ne 0) {
    Write-Error "git push failed"
    exit 1
}

Write-Output "Committed and pushed."
