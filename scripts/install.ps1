# Werkbord installer for Windows (experimental).
#
#   irm https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.ps1 | iex
#
# Downloads the release for this computer, checks it against the release's
# published checksums, installs werkbord.exe under %LOCALAPPDATA%\Programs\Werkbord
# (with devboard.cmd, its name before the rename, beside it) and puts that on your
# PATH, then runs `werkbord setup` (data directory, database,
# a scheduled task that starts the controller when you log on, phone access).
# It needs no account, no administrator rights and no Docker.
#
# Windows today: the controller, board, Git views, repository health, GitHub and
# phone access work. Coding agents (Claude Code, Codex) are started with Unix
# process control, so they run only where that exists: install inside WSL with the
# Linux installer to run agents.
#
# Environment: WERKBORD_VERSION (e.g. v1.2.3), WERKBORD_INSTALL_DIR, WERKBORD_BASE_URL,
# WERKBORD_NO_SETUP=1, and the WERKBORD_NO_* switches setup understands. Each is also
# read under its old DEVBOARD_ name.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# A WERKBORD_ variable, else its DEVBOARD_ name from before the rename.
function Opt($name) {
  $v = [Environment]::GetEnvironmentVariable("WERKBORD_$name")
  if (-not $v) { $v = [Environment]::GetEnvironmentVariable("DEVBOARD_$name") }
  return $v
}

$Repo = 'micho8cho93/werkbord'
$Base = if (Opt 'BASE_URL') { (Opt 'BASE_URL').TrimEnd('/') } else { "https://github.com/$Repo/releases" }

function Fail($msg) { Write-Error "werkbord install: $msg"; exit 1 }

# ---- this computer ----
$archName = if (Opt 'ARCH') { Opt 'ARCH' } else { $env:PROCESSOR_ARCHITECTURE }
switch -Regex ($archName) {
  '^(AMD64|x86_64|amd64)$' { $arch = 'amd64'; break }
  '^(ARM64|arm64|aarch64)$' { $arch = 'arm64'; break }
  default { Fail "no release is built for $archName. Build from source instead: https://github.com/$Repo" }
}
if ([Environment]::OSVersion.Platform -ne 'Win32NT' -and -not (Opt 'ARCH')) { Fail 'this is the Windows installer; on macOS and Linux use install.sh' }

# ---- which release ----
# A release is named by its product's tag (werkbord-v1.2.3; the earliest releases were a
# bare v1.2.3). The executable reports the bare version, v1.2.3.
$tag = Opt 'VERSION'
$explicit = [bool]$tag
if (-not $tag) {
  Write-Host 'Looking for the latest release...'
  # /latest answers with a redirect to /tag/<version>; read it without following it.
  $handler = New-Object System.Net.Http.HttpClientHandler
  $handler.AllowAutoRedirect = $false
  $client = New-Object System.Net.Http.HttpClient($handler)
  try {
    $resp = $client.GetAsync("$Base/latest").GetAwaiter().GetResult()
    $loc = $resp.Headers.Location
    if (-not $loc) { Fail "could not look up the latest release at $Base/latest" }
    $tag = ($loc.ToString().TrimEnd('/') -split '/')[-1]
  } finally { $client.Dispose() }
}
if ($tag -match '^werkbord-team-') { Fail "`"$tag`" is a Werkbord Team release. This installer is for the individual product; Team has its own." }
if ($tag -match '^werkbord-(v\d+\.\d+\.\d+.*)$') {
  $version = $Matches[1]
} elseif ($tag -match '^v?(\d+)\.(\d+)\.\d+') {
  $version = "v$($tag.TrimStart('v'))"
  if ($explicit) {
    # Asked for by version: releases before 0.8.0 have the bare tag, later ones the product tag.
    if ([int]$Matches[1] -eq 0 -and [int]$Matches[2] -lt 8) { $tag = $version } else { $tag = "werkbord-$version" }
  }
} else {
  Fail "`"$tag`" is not a release version (expected something like v1.2.3)"
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("werkbord-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "Installing Werkbord $version for windows/$arch"
  Invoke-WebRequest -UseBasicParsing -Uri "$Base/download/$tag/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
  # Releases are werkbord_...; a release from before the rename has only devboard_...
  $want = $null
  foreach ($prefix in @('werkbord', 'devboard')) {
    $candidate = "$($prefix)_$($version.TrimStart('v'))_windows_$arch.zip"
    foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
      $f = $line -split '\s+'
      if ($f.Count -ge 2 -and ($f[1].TrimStart('*')) -eq $candidate) { $want = $f[0] }
    }
    if ($want) { $asset = $candidate; break }
  }
  if (-not $want) { Fail "release $tag has no build for windows/$arch" }
  $zip = Join-Path $tmp $asset
  Invoke-WebRequest -UseBasicParsing -Uri "$Base/download/$tag/$asset" -OutFile $zip
  $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLower()
  if ($got -ne $want.ToLower()) { Fail "the download does not match its published checksum, so it was not installed`n  expected $want`n  got      $got" }

  Expand-Archive -Path $zip -DestinationPath (Join-Path $tmp 'unpack') -Force
  $exe = Get-ChildItem -Path (Join-Path $tmp 'unpack') -Recurse -Include 'werkbord.exe', 'devboard.exe' | Select-Object -First 1
  if (-not $exe) { Fail "$asset does not contain werkbord.exe" }
  $reported = (& $exe.FullName version 2>$null | Out-String).Trim()
  if ($reported -ne $version) { Fail "the downloaded executable says it is `"$reported`", not $version: not installing it" }

  # An existing install, under either name: it was devboard.exe in Programs\Devboard before the rename.
  $existing = $null
  $dirs = if (Opt 'INSTALL_DIR') { @(Opt 'INSTALL_DIR') } else { @((Join-Path $env:LOCALAPPDATA 'Programs\Werkbord'), (Join-Path $env:LOCALAPPDATA 'Programs\Devboard')) }
  foreach ($d in $dirs) {
    foreach ($n in @('werkbord.exe', 'devboard.exe')) {
      if (-not $existing -and (Test-Path (Join-Path $d $n))) { $existing = Join-Path $d $n }
    }
  }
  if ($existing) {
    Write-Host 'Upgrading the existing installation with verified service recovery...'
    & $exe.FullName install-release ([IO.Path]::GetFullPath($existing))
    if ($LASTEXITCODE -ne 0) { Fail 'upgrade did not complete; inspect the recovery message above. The installer did not replace the executable directly.' }
    exit 0
  }
  $dir = $dirs[0]
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $dest = Join-Path $dir 'werkbord.exe'
  # Clean installation; upgrades use the recovery lifecycle above.
  Copy-Item -Force $exe.FullName $dest
  # Its name before the rename, so scripts and habits keep working.
  $alias = Join-Path $dir 'devboard.cmd'
  if (-not (Test-Path $alias)) { Set-Content -Path $alias -Value "@`"%~dp0werkbord.exe`" %*" -Encoding ASCII }
  Write-Host "Installed $dest"

  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $dir), 'User')
    $env:Path = $env:Path + ';' + $dir
    Write-Host "Added $dir to your PATH (new terminals pick it up)."
  }
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host ''
Write-Host 'Note: on Windows the controller, board, Git views and phone access work; coding agents run only inside WSL (use the Linux installer there).'
if (Opt 'NO_SETUP') { Write-Host "Done. Run `"$dest setup`" to finish."; exit 0 }
& $dest setup
