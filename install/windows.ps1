# ctty Windows Installation Script
# Usage: 
#   Online:  irm https://raw.githubusercontent.com/zsuroy/ctty/master/install/windows.ps1 | iex
#   Local:   .\install\windows.ps1 -LocalBinary ".\ctty.exe"

param(
    [string]$InstallDir = "$env:LOCALAPPDATA\ctty",
    [switch]$Force = $false,
    [string]$LocalBinary = ""
)

$ErrorActionPreference = "Stop"

# Colors for output
function Write-ColorOutput($ForegroundColor) {
    $fc = $host.UI.RawUI.ForegroundColor
    $host.UI.RawUI.ForegroundColor = $ForegroundColor
    if ($args) {
        Write-Output $args
    }
    $host.UI.RawUI.ForegroundColor = $fc
}

function Write-Info { Write-ColorOutput Green $args }
function Write-Warning { Write-ColorOutput Yellow $args }
function Write-Error { Write-ColorOutput Red $args }

Write-Info "🚀 Installing ctty - Connection Manager"
Write-Info ""

# Check if ctty is already installed
$existingctty = Get-Command ctty -ErrorAction SilentlyContinue
if ($existingctty -and -not $Force) {
    $currentVersion = & ctty --version 2>$null | Select-String "version" | ForEach-Object { $_.ToString().Split()[-1] }
    Write-Warning "ctty is already installed (version: $currentVersion)"
    $response = Read-Host "Do you want to continue with the installation? (y/N)"
    if ($response -ne "y" -and $response -ne "Y") {
        Write-Info "Installation cancelled."
        exit 0
    }
}

# Detect architecture
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
Write-Info "Detected platform: Windows ($arch)"

# Check if using local binary
if ($LocalBinary -ne "") {
    if (-not (Test-Path $LocalBinary)) {
        Write-Error "Local binary not found: $LocalBinary"
        exit 1
    }
    
    Write-Info "Using local binary: $LocalBinary"
    $targetPath = "$InstallDir\ctty.exe"
    
    # Create installation directory
    if (-not (Test-Path $InstallDir)) {
        Write-Info "Creating installation directory: $InstallDir"
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    
    # Copy local binary
    Write-Info "Installing binary to: $targetPath"
    Copy-Item -Path $LocalBinary -Destination $targetPath -Force
    
} else {
    # Online installation
    Write-Info "Starting online installation..."
    $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("ctty-install-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    
    try {
        # Get latest version
        Write-Info "Fetching latest version..."
        try {
            $latestRelease = Invoke-RestMethod -Uri "https://api.github.com/repos/zsuroy/ctty/releases/latest"
            $latestVersion = $latestRelease.tag_name
            Write-Info "Target version: $latestVersion"
        } catch {
            throw "Failed to fetch version information: $($_.Exception.Message)"
        }

        # Download binary and the GoReleaser checksum manifest.
        $goreleaserArch = if ($arch -eq "amd64") { "x86_64" } else { "i386" }
        $fileName = "ctty_Windows_$goreleaserArch.zip"
        $releaseBase = "https://github.com/zsuroy/ctty/releases/download/$latestVersion"
        $downloadUrl = "$releaseBase/$fileName"
        $tempFile = Join-Path $tempDir $fileName
        $checksumsFile = Join-Path $tempDir "checksums.txt"

        Write-Info "Downloading $fileName..."
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempFile
        Invoke-WebRequest -Uri "$releaseBase/checksums.txt" -OutFile $checksumsFile

        $escapedName = [regex]::Escape($fileName)
        $checksumLine = @(Get-Content $checksumsFile | Where-Object { $_ -match "^([0-9A-Fa-f]{64})\s+$escapedName$" })
        if ($checksumLine.Count -ne 1) {
            throw "No unique SHA-256 checksum found for $fileName"
        }
        $expectedChecksum = ($checksumLine[0] -split '\s+')[0]
        $actualChecksum = (Get-FileHash -Path $tempFile -Algorithm SHA256).Hash
        if (-not $actualChecksum.Equals($expectedChecksum, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "Checksum verification failed for $fileName"
        }
        Write-Info "Checksum verified."

        # Create installation directory
        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }

        # Extract archive inside this invocation's private temporary directory.
        Write-Info "Extracting..."
        $extractDir = Join-Path $tempDir "extracted"
        Expand-Archive -Path $tempFile -DestinationPath $extractDir -Force
        # GoReleaser extracts the binary as just "ctty.exe", not with platform suffix
        $extractedBinary = Join-Path $extractDir "ctty.exe"
        if (-not (Test-Path -LiteralPath $extractedBinary -PathType Leaf)) {
            throw "Archive does not contain ctty.exe"
        }
        $targetPath = "$InstallDir\ctty.exe"
        
        Move-Item -Path $extractedBinary -Destination $targetPath -Force
    } catch {
        Write-Error $_.Exception.Message
        exit 1
    } finally {
        Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Check PATH
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    Write-Warning "The directory $InstallDir is not in your PATH."
    Write-Info "Adding to user PATH..."
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    Write-Info "Please restart your terminal to use the 'ctty' command."
}

Write-Info ""
Write-Info "✅ ctty successfully installed to: $targetPath"
Write-Info "You can now use the 'ctty' command!"

# Verify installation
if (Test-Path $targetPath) {
    Write-Info ""
    Write-Info "Verifying installation..."
    & $targetPath --version
}
