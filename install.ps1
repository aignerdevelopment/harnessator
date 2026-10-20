# harnessator installer for Windows - run from PowerShell:
#
#   powershell -c "irm https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.ps1 | iex"
#
# Installs the latest GitHub Release binary to %USERPROFILE%\.local\bin
# (no admin), sha256-verified against the release checksums file. Re-run
# to update. -Version pins a specific tag; -InstallDir overrides the
# location. Never touches the registry, never runs elevated.
param(
  [string]$Version = "",
  [string]$InstallDir = (Join-Path $env:USERPROFILE ".local\bin")
)
$ErrorActionPreference = "Stop"
$Repo = "aignerdevelopment/harnessator"
$Base = "https://github.com/$Repo/releases/download"

function Say($m) { Write-Host "harnessator-install: $m" }
function Die($m) { Write-Host "harnessator-install: error: $m" -ForegroundColor Red; exit 1 }

$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { Die "32-bit Windows is not supported" }

try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}

if (-not $Version) {
  Say "resolving the latest release"
  try {
    $rel = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $rel.tag_name
  } catch { Die "could not resolve the latest release: $_  (pass -Version)" }
}

$asset = "harnessator_${Version}_windows_${arch}.zip"
$tmp = Join-Path $env:TEMP ("harnessator-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

try {
  $zip = Join-Path $tmp $asset
  Say "fetching $asset"
  Invoke-WebRequest -Uri "$Base/$Version/$asset" -OutFile $zip -UseBasicParsing
  $sums = Join-Path $tmp "checksums.txt"
  Invoke-WebRequest -Uri "$Base/$Version/harnessator_${Version}_checksums.txt" -OutFile $sums -UseBasicParsing

  $line = (Get-Content $sums | Select-String ([regex]::Escape($asset) + '(\s|$)') | Select-Object -Last 1)
  if (-not $line) { Die "no checksum line for $asset - refusing" }
  $want = ($line.Line -split '\s+')[0]
  $got = (Get-FileHash -Algorithm SHA256 $zip).Hash.ToLower()
  if ($want -ne $got) { Die "checksum MISMATCH for $asset (want $want, got $got) - refusing to install" }
  Say "checksum verified"

  Expand-Archive -LiteralPath $zip -DestinationPath $tmp
  $exeName = "harnessator_${Version}_windows_${arch}.exe"
  $exe = Join-Path $tmp $exeName
  if (-not (Test-Path $exe)) { Die "$exeName not found in the archive" }

  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  $dest = Join-Path $InstallDir "harnessator.exe"
  Copy-Item $exe $dest -Force
  Say "installed -> $dest"

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($userPath -split ';') -notcontains $InstallDir) {
    Say "note: $InstallDir is not on your PATH. Add it with:"
    Say "  [Environment]::SetEnvironmentVariable('Path', \"$InstallDir;\" + [Environment]::GetEnvironmentVariable('Path','User'), 'User')"
  }
  & $dest version | ForEach-Object { Say "next: $_" }
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
