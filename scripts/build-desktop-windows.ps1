param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$OutputDirectory
)

$ErrorActionPreference = 'Stop'

function Invoke-Native {
    param([Parameter(Mandatory = $true)][string]$File, [string[]]$Arguments = @())
    & $File @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$File exited with status $LASTEXITCODE" }
}

if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Version must be a stable semantic version.' }
if ([string]::IsNullOrWhiteSpace($env:LICENSE_ISSUER_PUBLIC_KEY) -or $env:LICENSE_ISSUER_PUBLIC_KEY -notmatch '^[A-Za-z0-9_-]{43}$') {
    throw 'LICENSE_ISSUER_PUBLIC_KEY must be the 32-byte raw URL base64 public key.'
}
if ([string]::IsNullOrWhiteSpace($env:WINDOWS_SIGNING_CERTIFICATE_PFX) -or [string]::IsNullOrWhiteSpace($env:WINDOWS_SIGNING_CERTIFICATE_PASSWORD)) {
    throw 'The Windows Authenticode certificate and password are required for a stable release.'
}

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$originalLocation = Get-Location
$desktop = Join-Path $root 'desktop'
$wailsConfigPath = Join-Path $desktop 'wails.json'
$originalWailsConfig = [System.IO.File]::ReadAllText($wailsConfigPath)
$stage = Join-Path ([System.IO.Path]::GetTempPath()) ("werkbord-windows-" + [guid]::NewGuid().ToString('N'))
$output = [System.IO.Path]::GetFullPath($OutputDirectory)
$importedCertificates = @()
New-Item -ItemType Directory -Force -Path $stage, $output | Out-Null

try {
    Set-Location $root
    $config = $originalWailsConfig | ConvertFrom-Json
    $config.info.productVersion = $Version
    [System.IO.File]::WriteAllText($wailsConfigPath, ($config | ConvertTo-Json -Depth 20) + "`n", [System.Text.UTF8Encoding]::new($false))

    $pfxPath = $env:WINDOWS_SIGNING_CERTIFICATE_PFX
    $password = ConvertTo-SecureString $env:WINDOWS_SIGNING_CERTIFICATE_PASSWORD -AsPlainText -Force
    $importedCertificates = @(Import-PfxCertificate -FilePath $pfxPath -CertStoreLocation Cert:\CurrentUser\My -Password $password)
    $certificate = @($importedCertificates | Where-Object {
        $_.HasPrivateKey -and @($_.EnhancedKeyUsageList | ForEach-Object { $_.ObjectId.Value }) -contains '1.3.6.1.5.5.7.3.3'
    }) | Select-Object -First 1
    if ($null -eq $certificate) { throw 'The PFX does not contain a code-signing certificate with a private key.' }
    $thumbprint = $certificate.Thumbprint.ToUpperInvariant()
    if ($thumbprint -notmatch '^[A-F0-9]{40}$') { throw 'The certificate has an invalid Authenticode thumbprint.' }

    $sdk = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $signTool = Get-ChildItem -Path $sdk -Filter signtool.exe -Recurse -ErrorAction Stop |
        Where-Object { $_.Directory.Name -eq 'x64' } |
        Sort-Object { [version]$_.Directory.Parent.Name } -Descending |
        Select-Object -First 1
    if ($null -eq $signTool) { throw 'The Windows SDK signtool.exe was not found.' }

    $nebulaManifest = [System.IO.File]::ReadAllText((Join-Path $root 'internal/team/infra/nebula/manifest.go'))
    $rqliteManifest = [System.IO.File]::ReadAllText((Join-Path $root 'internal/team/infra/rqlite/manifest.go'))
    $nebulaVersion = [regex]::Match($nebulaManifest, 'const Version = "([^"]+)"').Groups[1].Value
    $rqliteVersion = [regex]::Match($rqliteManifest, 'const Version = "([^"]+)"').Groups[1].Value
    if (!$nebulaVersion -or !$rqliteVersion) { throw 'The pinned Team payload versions could not be read.' }
    Invoke-Native 'bash' @('scripts/fetch-nebula.sh', 'windows', 'amd64')
    Invoke-Native 'bash' @('scripts/fetch-rqlite.sh', 'windows', 'amd64')
    $nebula = Join-Path $root ".cache/nebula/$nebulaVersion/windows_amd64"
    $rqlite = Join-Path $root ".cache/rqlite/$rqliteVersion/windows_amd64"

    $app = Join-Path $stage 'app'
    $bin = Join-Path $app 'bin'
    $helpers = Join-Path $app 'Helpers'
    $resources = Join-Path $app 'Resources'
    $licenses = Join-Path $resources 'licenses'
    New-Item -ItemType Directory -Force -Path $bin, $helpers, $licenses | Out-Null

    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $controller = Join-Path $helpers 'werkbord.exe'
    $ldflags = "-s -w -X main.version=v$Version"
    Invoke-Native 'go' @('build', '-trimpath', '-ldflags', $ldflags, '-o', $controller, './cmd/werkbord')

    $rqlited = Join-Path $helpers 'rqlited.exe'
    Copy-Item (Join-Path $rqlite 'rqlited.exe') $rqlited
    $databaseHash = (Get-FileHash -Algorithm SHA256 $rqlited).Hash.ToLowerInvariant()
    $rqliteCommit = [regex]::Match($rqliteManifest, 'const SourceCommit = "([^"]+)"').Groups[1].Value
    $buildRecord = @{ version = "v$rqliteVersion"; commit = $rqliteCommit; sha256 = $databaseHash } | ConvertTo-Json -Compress
    [System.IO.File]::WriteAllText((Join-Path $resources 'rqlited.build'), $buildRecord + "`n", [System.Text.UTF8Encoding]::new($false))

    Copy-Item (Join-Path $nebula 'nebula.exe') (Join-Path $helpers 'nebula.exe')
    Copy-Item (Join-Path $nebula 'wintun.dll') (Join-Path $helpers 'wintun.dll')
    Copy-Item (Join-Path $nebula 'licenses/WINTUN-LICENSE.txt') (Join-Path $licenses 'WINTUN-LICENSE.txt')
    Copy-Item (Join-Path $root 'third_party/nebula') (Join-Path $licenses 'nebula') -Recurse
    Copy-Item (Join-Path $root 'third_party/rqlite') (Join-Path $licenses 'rqlite') -Recurse
    Copy-Item (Join-Path $root 'third_party/go') (Join-Path $licenses 'go') -Recurse

    $team = Join-Path $helpers 'werkbord-team.exe'
    $teamLdFlags = "-s -w -X main.version=v$Version -X main.licenseIssuer=$env:LICENSE_ISSUER_PUBLIC_KEY"
    Invoke-Native 'go' @('build', '-trimpath', '-ldflags', $teamLdFlags, '-o', $team, './cmd/werkbord-team')

    Push-Location $desktop
    try {
        $windowLdFlags = "-s -w -X main.version=v$Version -X devboard/desktop/internal/teaminstall.releaseBuild=v$Version -X devboard/desktop/internal/teaminstall.signingThumbprint=$thumbprint"
        Invoke-Native 'go' @('run', 'github.com/wailsapp/wails/v2/cmd/wails@v2.17.0', 'build', '-platform', 'windows/amd64', '-skipfrontend', '-skipbindings', '-nosyncgo', '-m', '-tags', 'desktop,production', '-webview2', 'embed', '-o', 'Werkbord.exe', '-ldflags', $windowLdFlags)
    } finally { Pop-Location }
    Copy-Item (Join-Path $desktop 'build/bin/Werkbord.exe') (Join-Path $bin 'Werkbord.exe')

    foreach ($path in @($controller, $team, (Join-Path $bin 'Werkbord.exe'))) {
        Invoke-Native $signTool.FullName @('sign', '/fd', 'SHA256', '/td', 'SHA256', '/tr', 'http://timestamp.digicert.com', '/f', $pfxPath, '/p', $env:WINDOWS_SIGNING_CERTIFICATE_PASSWORD, $path)
        Invoke-Native $signTool.FullName @('verify', '/pa', '/all', $path)
    }

    $verify = & (Join-Path $bin 'Werkbord.exe') '--verify-release' | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or !$verify.ok -or $verify.version -ne "v$Version") {
        throw "The signed app failed its release verification: $($verify.detail)"
    }
    $controllerVersion = (& $controller 'version' | Select-Object -Last 1).Trim()
    if ($LASTEXITCODE -ne 0 -or $controllerVersion -ne "v$Version") {
        throw 'The controller helper reports a different version.'
    }
    $teamVersion = (& $team 'version' | Select-Object -Last 1).Trim()
    if ($LASTEXITCODE -ne 0 -or $teamVersion -ne "v$Version") {
        throw "Team's service reports a different version."
    }

    $archive = Join-Path $output "Werkbord_${Version}_windows_amd64.zip"
    if (Test-Path $archive) { Remove-Item -LiteralPath $archive }
    Compress-Archive -Path (Join-Path $app '*') -DestinationPath $archive -CompressionLevel Optimal

    $installer = Join-Path $output "Werkbord_${Version}_windows_amd64_setup.exe"
    $icon = Join-Path $desktop 'build/windows/icon.ico'
    $nsis = Join-Path $desktop 'build/windows/installer/werkbord.nsi'
    Invoke-Native 'makensis' @("-DVERSION=$Version", "-DAPP_SRC=$app", "-DAPP_ICON=$icon", "-DOUTPUT_FILE=$installer", $nsis)
    Invoke-Native $signTool.FullName @('sign', '/fd', 'SHA256', '/td', 'SHA256', '/tr', 'http://timestamp.digicert.com', '/f', $pfxPath, '/p', $env:WINDOWS_SIGNING_CERTIFICATE_PASSWORD, $installer)
    Invoke-Native $signTool.FullName @('verify', '/pa', '/all', $installer)

    foreach ($path in @($archive, $installer)) {
        $hash = (Get-FileHash -Algorithm SHA256 $path).Hash.ToLowerInvariant()
        [System.IO.File]::WriteAllText("$path.sha256", "$hash  $(Split-Path -Leaf $path)`n", [System.Text.UTF8Encoding]::new($false))
    }
} finally {
    [System.IO.File]::WriteAllText($wailsConfigPath, $originalWailsConfig, [System.Text.UTF8Encoding]::new($false))
    if (Test-Path $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
    foreach ($thumb in $importedCertificates.Thumbprint) {
        Remove-Item -Path "Cert:\CurrentUser\My\$thumb" -ErrorAction SilentlyContinue
    }
    Set-Location $originalLocation
}
