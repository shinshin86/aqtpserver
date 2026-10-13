param([string]$Version = "latest")

$ErrorActionPreference = "Stop"
$Owner = "shinshin86"
$Repo = "aqtpserver"
$BinName = "aqtpserver.exe"
$BinDir = if ($env:BIN_DIR) { $env:BIN_DIR } else { "$env:LOCALAPPDATA\Programs\aqtpserver" }

function Invoke-Download {
    param([string]$Uri, [string]$OutFile)
    $params = @{ Uri = $Uri; OutFile = $OutFile; MaximumRedirection = 5 }
    if ($PSVersionTable.PSVersion.Major -lt 6) { $params.UseBasicParsing = $true }
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try { Invoke-WebRequest @params; return }
        catch {
            if ($attempt -eq 3) { throw }
            Start-Sleep -Seconds (2 * $attempt)
        }
    }
}

if ($env:OS -ne "Windows_NT") { throw "This installer requires Windows." }
$MachineArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$Arch = switch ($MachineArch) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "Unsupported architecture: $MachineArch" }
}
if ($Version -ne "latest" -and $Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') {
    throw "Invalid version format. Use vX.Y.Z or 'latest'."
}
if ($Version -eq "latest") {
    $params = @{ Uri = "https://api.github.com/repos/$Owner/$Repo/releases/latest" }
    if ($PSVersionTable.PSVersion.Major -lt 6) { $params.UseBasicParsing = $true }
    $Version = (Invoke-RestMethod @params).tag_name
}
if (-not $Version -or $Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') {
    throw "Failed to determine a release version."
}

$Asset = "aqtpserver_${Version}_windows_${Arch}.zip"
$BaseUrl = "https://github.com/$Owner/$Repo/releases/download/$Version"
$TempDir = Join-Path ([IO.Path]::GetTempPath()) ("aqtpserver-install-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $TempDir | Out-Null
try {
    $ChecksumsPath = Join-Path $TempDir "checksums.txt"
    Invoke-Download -Uri "$BaseUrl/checksums.txt" -OutFile $ChecksumsPath
    $Pattern = '^([a-fA-F0-9]{64})\s+' + [regex]::Escape($Asset) + '$'
    $ChecksumLines = @(Select-String -Path $ChecksumsPath -Pattern $Pattern)
    if ($ChecksumLines.Count -ne 1) { throw "Missing or invalid checksum for $Asset." }
    $ExpectedChecksum = $ChecksumLines[0].Matches[0].Groups[1].Value
    $AssetPath = Join-Path $TempDir $Asset
    Write-Host "Downloading $Asset..."
    Invoke-Download -Uri "$BaseUrl/$Asset" -OutFile $AssetPath
    $ActualChecksum = (Get-FileHash $AssetPath -Algorithm SHA256).Hash
    if ($ExpectedChecksum -ne $ActualChecksum) { throw "Checksum mismatch for $Asset." }
    Expand-Archive -Path $AssetPath -DestinationPath $TempDir
    $BinPath = Join-Path $TempDir $BinName
    if (-not (Test-Path -LiteralPath $BinPath -PathType Leaf)) { throw "Binary missing from release archive." }
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $TargetPath = Join-Path $BinDir $BinName
    if (Test-Path -LiteralPath $TargetPath) {
        Copy-Item -LiteralPath $TargetPath -Destination "$TargetPath.bak" -Force
        Write-Host "Existing binary backed up to $BinName.bak"
    }
    Copy-Item -LiteralPath $BinPath -Destination $TargetPath -Force
    $PathValue = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($PathValue -split ';') -notcontains $BinDir) {
        [Environment]::SetEnvironmentVariable("Path", "$BinDir;$PathValue", "User")
        Write-Host "Added $BinDir to your user PATH. Restart your terminal to apply."
    }
    Write-Host "Installed $BinName to $BinDir"
    Write-Host "Run 'aqtpserver --version' to verify. Install AquesTalkPlayer separately."
} finally {
    Remove-Item -LiteralPath $TempDir -Recurse -Force
}
