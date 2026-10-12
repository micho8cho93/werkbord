param([Parameter(Mandatory = $true)][string]$Tag)

$ErrorActionPreference = 'Stop'
if ($Tag -notmatch '^werkbord-v\d+\.\d+\.\d+$') { throw "Invalid stable release tag: $Tag" }
$version = $Tag.Substring('werkbord-v'.Length)
$base = $env:WERKBORD_RELEASE_BASE
if ([string]::IsNullOrWhiteSpace($base)) { $base = 'https://github.com/micho8cho93/werkbord/releases/download' }
$work = Join-Path $env:RUNNER_TEMP ("werkbord-public-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null

function Download-PublicAsset([string]$Name) {
    $url = "$base/$Tag/$Name"
    & curl.exe -fsSL --retry 6 --retry-delay 10 --retry-all-errors -o (Join-Path $work $Name) $url
    if ($LASTEXITCODE -ne 0) { throw "The public release does not serve $Name ($url)." }
}

function Verify-Sidecar([string]$Name) {
    $asset = Join-Path $work $Name
    $sidecar = Get-Content "$asset.sha256" -Raw
    if ($sidecar -notmatch '^([0-9a-fA-F]{64})\s+\S+') { throw "Invalid SHA-256 sidecar for $Name." }
    $want = $Matches[1].ToLowerInvariant()
    $have = (Get-FileHash -Algorithm SHA256 $asset).Hash.ToLowerInvariant()
    if ($want -ne $have) { throw "$Name has SHA-256 $have, but the public sidecar says $want." }
}

function Require-Authenticode([string]$Path, [string]$Thumbprint) {
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Thumbprint -ne $Thumbprint) {
        throw "The signature on $(Split-Path -Leaf $Path) is invalid or is from a different release certificate."
    }
}

try {
    $stem = "Werkbord_${version}_windows_amd64"
    $zipName = "$stem.zip"
    $setupName = "${stem}_setup.exe"
    Download-PublicAsset $zipName
    Download-PublicAsset "$zipName.sha256"
    Download-PublicAsset $setupName
    Download-PublicAsset "$setupName.sha256"
    Verify-Sidecar $zipName
    Verify-Sidecar $setupName

    $setup = Join-Path $work $setupName
    $setupSignature = Get-AuthenticodeSignature -LiteralPath $setup
    if ($setupSignature.Status -ne 'Valid') { throw 'The public Windows installer has no valid Authenticode signature.' }
    $thumbprint = $setupSignature.SignerCertificate.Thumbprint
    $unzipped = Join-Path $work 'zip'
    Expand-Archive -LiteralPath (Join-Path $work $zipName) -DestinationPath $unzipped
    foreach ($relative in @('bin\Werkbord.exe', 'Helpers\werkbord.exe', 'Helpers\werkbord-team.exe')) {
        Require-Authenticode (Join-Path $unzipped $relative) $thumbprint
    }

    $process = Start-Process -FilePath $setup -ArgumentList '/S' -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "The signed setup exited with status $($process.ExitCode)." }
    $install = Join-Path ${env:ProgramFiles} 'Werkbord'
    $app = Join-Path $install 'bin\Werkbord.exe'
    $report = (& $app '--verify-release' | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0 -or !$report.ok -or $report.version -ne "v$version") {
        throw "The installed app failed its release trust check: $($report.detail)"
    }
    foreach ($name in @('werkbord.exe', 'werkbord-team.exe')) {
        $helper = Join-Path $install "Helpers\$name"
        Require-Authenticode $helper $thumbprint
        $reported = (& $helper 'version' | Select-Object -Last 1).Trim()
        if ($LASTEXITCODE -ne 0 -or $reported -ne "v$version") { throw "$name reports $reported, expected v$version." }
    }
    $uninstall = Start-Process -FilePath (Join-Path $install 'Uninstall.exe') -ArgumentList '/S' -Wait -PassThru
    if ($uninstall.ExitCode -ne 0) { throw "The app uninstaller exited with status $($uninstall.ExitCode)." }
    Write-Output "verify-windows-desktop-release: public $version installer and app signatures, package hashes, install and uninstall all passed"
} finally {
    if (Test-Path $work) { Remove-Item -LiteralPath $work -Recurse -Force }
}
