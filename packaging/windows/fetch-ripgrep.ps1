#requires -Version 7.0
<#
.SYNOPSIS
Downloads the pinned official Windows amd64 ripgrep archive, or verifies that a release ZIP contains it.
The executable and upstream license texts are accepted only when their SHA-256 values match the manifest.
#>
[CmdletBinding()]
param(
    [string] $OutputDirectory = '',
    [string] $AssertReleaseArchive = '',
    [string] $AssertReleaseOmitsArchive = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$modes = @($OutputDirectory, $AssertReleaseArchive, $AssertReleaseOmitsArchive)
$modes = @($modes | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
if ($modes.Count -ne 1) {
    throw 'Specify exactly one of -OutputDirectory, -AssertReleaseArchive, or -AssertReleaseOmitsArchive.'
}

function Get-RipgrepManifest {
    $path = Join-Path $PSScriptRoot 'third_party\ripgrep\manifest.json'
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Ripgrep manifest was not found: $path" }
    $manifest = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
    foreach ($name in @(
        'name', 'version', 'target', 'archive_url', 'archive_sha256', 'archive_root',
        'executable', 'executable_sha256', 'executable_bytes', 'license', 'payload_dir'
    )) {
        if ([string]::IsNullOrWhiteSpace([string] $manifest.$name)) { throw "Ripgrep manifest is missing $name." }
    }
    if ([string] $manifest.name -ne 'ripgrep') { throw 'Ripgrep manifest name is not ripgrep.' }
    if ([string] $manifest.executable -ne 'rg.exe') { throw 'Ripgrep manifest executable must be rg.exe.' }
    if ([string] $manifest.license -ne 'Unlicense OR MIT') { throw 'Ripgrep manifest license must stay Unlicense OR MIT.' }
    if ([string] $manifest.payload_dir -ne 'third_party/ripgrep') { throw 'Ripgrep payload directory must stay third_party/ripgrep.' }
    if ([string] $manifest.archive_root -ne "ripgrep-$($manifest.version)-$($manifest.target)") { throw 'Ripgrep archive root does not match the pinned version.' }
    $expectedUrl = "https://github.com/BurntSushi/ripgrep/releases/download/$($manifest.version)/ripgrep-$($manifest.version)-$($manifest.target).zip"
    if ([string] $manifest.archive_url -ne $expectedUrl) { throw 'Ripgrep archive URL does not match the pinned version and target.' }
    foreach ($hashName in @('archive_sha256', 'executable_sha256')) {
        if ([string] $manifest.$hashName -notmatch '^[0-9a-f]{64}$') { throw "Ripgrep manifest $hashName is not a lowercase SHA-256." }
    }
    if ([int64] $manifest.executable_bytes -le 0 -or [int64] $manifest.executable_bytes -gt 32MB) { throw 'Ripgrep executable size is outside the accepted range.' }
    if ($null -eq $manifest.license_files) { throw 'Ripgrep manifest is missing license_files.' }
    foreach ($licenseName in @('COPYING', 'LICENSE-MIT', 'UNLICENSE')) {
        $hash = [string] $manifest.license_files.$licenseName
        if ($hash -notmatch '^[0-9a-f]{64}$') { throw "Ripgrep license hash is missing or invalid: $licenseName" }
    }
    $licenseCount = @($manifest.license_files.PSObject.Properties).Count
    if ($licenseCount -ne 3) { throw "Ripgrep manifest must pin exactly the three upstream license files, found $licenseCount." }
    return $manifest
}

function New-RipgrepNotice([pscustomobject] $Manifest) {
    $lines = @(
        'AgentDock bundles the official, unmodified ripgrep Windows amd64 executable for search_text.'
        ''
        'Project: ripgrep'
        "Version: $($Manifest.version)"
        'Upstream: https://github.com/BurntSushi/ripgrep'
        "License: $($Manifest.license)"
        "Artifact: ripgrep-$($Manifest.version)-$($Manifest.target).zip"
        "Archive SHA-256: $($Manifest.archive_sha256)"
        'Executable: rg.exe'
        "Executable SHA-256: $($Manifest.executable_sha256)"
        ''
        'License texts from that artifact are included beside rg.exe: COPYING, LICENSE-MIT, and UNLICENSE.'
        'AgentDock does not modify ripgrep.'
    )
    return (($lines -join "`n") + "`n")
}

function Get-ZipEntryByName([IO.Compression.ZipArchive] $Archive, [string] $Name) {
    foreach ($entry in $Archive.Entries) {
        $normalized = $entry.FullName.Replace('\', '/')
        if ($normalized -eq $Name) { return $entry }
    }
    return $null
}

function Read-ZipEntryBytes([IO.Compression.ZipArchive] $Archive, [string] $Name, [int64] $MaxBytes) {
    if ($Name.Contains('..') -or $Name.StartsWith('/') -or $Name.Contains('\')) { throw "Unsafe ripgrep archive entry: $Name" }
    $entry = Get-ZipEntryByName -Archive $Archive -Name $Name
    if ($null -eq $entry) { throw "Archive entry was not found: $Name" }
    if ($entry.Length -le 0 -or $entry.Length -gt $MaxBytes) { throw "Archive entry size is not accepted: $Name" }
    $stream = $entry.Open()
    try {
        $buffer = New-Object byte[] ([int] $entry.Length)
        $offset = 0
        while ($offset -lt $buffer.Length) {
            $read = $stream.Read($buffer, $offset, $buffer.Length - $offset)
            if ($read -le 0) { throw "Archive entry ended early: $Name" }
            $offset += $read
        }
        return $buffer
    } finally {
        $stream.Dispose()
    }
}

function Get-Sha256Hex([byte[]] $Bytes) {
    $sha = [Security.Cryptography.SHA256]::Create()
    try { return [Convert]::ToHexString($sha.ComputeHash($Bytes)).ToLowerInvariant() }
    finally { $sha.Dispose() }
}

function Open-ZipArchive([string] $Path) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Archive was not found: $Path" }
    return [IO.Compression.ZipFile]::OpenRead((Resolve-Path -LiteralPath $Path).Path)
}

function Assert-RipgrepReleaseBytes([IO.Compression.ZipArchive] $Archive, [pscustomobject] $Manifest) {
    $prefix = [string] $Manifest.payload_dir
    $allowed = @(
        $prefix,
        "$prefix/",
        "$prefix/rg.exe",
        "$prefix/COPYING",
        "$prefix/LICENSE-MIT",
        "$prefix/UNLICENSE",
        "$prefix/NOTICE"
    )
    foreach ($entry in $Archive.Entries) {
        $name = $entry.FullName.Replace('\', '/')
        if ($name -eq $prefix -or $name.StartsWith("$prefix/")) {
            if ($allowed -notcontains $name) { throw "Release archive contains an unexpected ripgrep entry: $name" }
        }
    }
    $executable = Read-ZipEntryBytes -Archive $Archive -Name "$prefix/rg.exe" -MaxBytes 32MB
    if ($executable.LongLength -ne [int64] $Manifest.executable_bytes) { throw 'Bundled rg.exe size does not match the ripgrep manifest.' }
    $executableHash = Get-Sha256Hex -Bytes $executable
    if ($executableHash -ne [string] $Manifest.executable_sha256) { throw "Bundled rg.exe SHA-256 mismatch. Expected $($Manifest.executable_sha256), got $executableHash." }
    foreach ($licenseName in @('COPYING', 'LICENSE-MIT', 'UNLICENSE')) {
        $bytes = Read-ZipEntryBytes -Archive $Archive -Name "$prefix/$licenseName" -MaxBytes 1MB
        $hash = Get-Sha256Hex -Bytes $bytes
        $expected = [string] $Manifest.license_files.$licenseName
        if ($hash -ne $expected) { throw "Bundled ripgrep $licenseName SHA-256 mismatch. Expected $expected, got $hash." }
    }
    $noticeBytes = Read-ZipEntryBytes -Archive $Archive -Name "$prefix/NOTICE" -MaxBytes 1MB
    $notice = [Text.UTF8Encoding]::new($false).GetString($noticeBytes)
    $expectedNotice = New-RipgrepNotice -Manifest $Manifest
    if ($notice -ne $expectedNotice) { throw 'Bundled ripgrep NOTICE does not match the pinned manifest.' }
}

function Assert-ReleaseOmitsRipgrep([string] $ArchivePath) {
    $archive = Open-ZipArchive -Path $ArchivePath
    try {
        foreach ($entry in $archive.Entries) {
            $name = $entry.FullName.Replace('\', '/')
            if ($name -eq 'third_party/ripgrep' -or $name.StartsWith('third_party/ripgrep/')) {
                throw "This release archive must not include the Windows amd64 ripgrep payload: $name"
            }
        }
    } finally {
        $archive.Dispose()
    }
}

$manifest = Get-RipgrepManifest

if (-not [string]::IsNullOrWhiteSpace($AssertReleaseOmitsArchive)) {
    Assert-ReleaseOmitsRipgrep -ArchivePath $AssertReleaseOmitsArchive
    return
}

if (-not [string]::IsNullOrWhiteSpace($AssertReleaseArchive)) {
    $archive = Open-ZipArchive -Path $AssertReleaseArchive
    try { Assert-RipgrepReleaseBytes -Archive $archive -Manifest $manifest }
    finally { $archive.Dispose() }
    return
}

if (-not [IO.Path]::IsPathRooted($OutputDirectory)) {
    $OutputDirectory = [IO.Path]::GetFullPath((Join-Path (Get-Location) $OutputDirectory))
}
if (Test-Path -LiteralPath $OutputDirectory) { Remove-Item -LiteralPath $OutputDirectory -Recurse -Force }
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
$work = Join-Path ([IO.Path]::GetTempPath()) ('agentdock-ripgrep-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work -Force | Out-Null
try {
    $downloaded = Join-Path $work 'ripgrep.zip'
    for ($attempt = 1; $attempt -le 4; $attempt++) {
        try {
            Invoke-WebRequest -Uri ([string] $manifest.archive_url) -OutFile $downloaded -TimeoutSec 120
            break
        } catch {
            if ($attempt -eq 4) { throw }
            Start-Sleep -Seconds 2
        }
    }
    $archiveHash = (Get-FileHash -LiteralPath $downloaded -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($archiveHash -ne [string] $manifest.archive_sha256) {
        throw "Official ripgrep archive SHA-256 mismatch. Expected $($manifest.archive_sha256), got $archiveHash."
    }
    $archive = Open-ZipArchive -Path $downloaded
    try {
        $root = [string] $manifest.archive_root
        $executable = Read-ZipEntryBytes -Archive $archive -Name "$root/rg.exe" -MaxBytes 32MB
        if ($executable.LongLength -ne [int64] $manifest.executable_bytes) { throw 'Downloaded rg.exe size does not match the ripgrep manifest.' }
        $executableHash = Get-Sha256Hex -Bytes $executable
        if ($executableHash -ne [string] $manifest.executable_sha256) {
            throw "Downloaded rg.exe SHA-256 mismatch. Expected $($manifest.executable_sha256), got $executableHash."
        }
        [IO.File]::WriteAllBytes((Join-Path $OutputDirectory 'rg.exe'), $executable)
        foreach ($licenseName in @('COPYING', 'LICENSE-MIT', 'UNLICENSE')) {
            $bytes = Read-ZipEntryBytes -Archive $archive -Name "$root/$licenseName" -MaxBytes 1MB
            $hash = Get-Sha256Hex -Bytes $bytes
            $expected = [string] $manifest.license_files.$licenseName
            if ($hash -ne $expected) { throw "Downloaded ripgrep $licenseName SHA-256 mismatch. Expected $expected, got $hash." }
            [IO.File]::WriteAllBytes((Join-Path $OutputDirectory $licenseName), $bytes)
        }
    } finally {
        $archive.Dispose()
    }
    $utf8 = [Text.UTF8Encoding]::new($false)
    [IO.File]::WriteAllText((Join-Path $OutputDirectory 'NOTICE'), (New-RipgrepNotice -Manifest $manifest), $utf8)
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}
