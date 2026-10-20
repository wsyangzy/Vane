[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Tag,
    [string]$RepositoryRoot = (Split-Path -Parent (Split-Path -Parent $PSScriptRoot)),
    [string]$NotesPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($Tag -cnotmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') {
    throw "Release tag must use vMajor.Minor.Patch: $Tag"
}
$version = $Tag.Substring(1)
$sourcePath = Join-Path $RepositoryRoot 'internal/buildinfo/buildinfo.go'
$source = Get-Content -LiteralPath $sourcePath -Raw -Encoding utf8
$match = [regex]::Match($source, '(?m)^\s*(?:var\s+)?Version\s*=\s*"([^"]+)"')
if (-not $match.Success -or $match.Groups[1].Value -cne $version) {
    throw "Release tag $Tag does not match internal/buildinfo/buildinfo.go"
}

$catalogPath = Join-Path $RepositoryRoot 'frontend/src/app/release-notes.json'
$catalog = Get-Content -LiteralPath $catalogPath -Raw -Encoding utf8 | ConvertFrom-Json
if ($catalog.currentVersion -cne $version) {
    throw "Release tag $Tag does not match the release catalog currentVersion"
}
$releases = @($catalog.releases | Where-Object { $_.version -ceq $version })
if ($releases.Count -ne 1) {
    throw "Release notes must contain exactly one entry for $Tag"
}

$sections = @($releases[0].sections | ForEach-Object {
    $items = @($_.items) -join '；'
    "- **$($_.title)**：$items"
})
if ($sections.Count -eq 0) {
    throw "Release notes sections are empty for $Tag"
}

if ($NotesPath) {
    $body = @(
        "## Vane $Tag"
        ''
        '### 本次更新'
        ''
        ($sections -join "`n")
        ''
        '### 下载'
        ''
        '- Windows x64: Vane-windows-amd64.zip'
        '- SHA256 校验文件：checksums.txt'
    ) -join "`n"
    [System.IO.File]::WriteAllText($NotesPath, $body, [System.Text.UTF8Encoding]::new($false))
}

if ($env:GITHUB_OUTPUT) {
    [System.IO.File]::AppendAllText($env:GITHUB_OUTPUT, "version=$version`n", [System.Text.UTF8Encoding]::new($false))
}
Write-Output "Validated Vane $Tag"
