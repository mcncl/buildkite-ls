#!/bin/bash

set -e

# Buildkite Language Server Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/mcncl/buildkite-ls/main/install.sh | bash

REPO="mcncl/buildkite-ls"
BINARY_NAME="buildkite-ls"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Detect OS and architecture
detect_platform() {
    local os arch
    
    case "$(uname -s)" in
        Linux*)
            os="linux"
            ;;
        Darwin*)
            os="darwin"
            ;;
        CYGWIN*|MINGW*|MSYS*)
            os="windows"
            ;;
        *)
            error "Unsupported operating system: $(uname -s)"
            ;;
    esac
    
    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        *)
            error "Unsupported architecture: $(uname -m)"
            ;;
    esac
    
    echo "${os}_${arch}"
}

# Get latest release version
get_latest_version() {
    local version
    version=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    
    if [ -z "$version" ]; then
        error "Failed to get latest version"
    fi
    
    echo "$version"
}

# Download and install
install_buildkite_ls() {
    local platform version download_url tmp_dir archive_name
    
    platform=$(detect_platform)
    version=$(get_latest_version)
    
    info "Installing buildkite-ls ${version} for ${platform}"
    
    # Create temporary directory
    tmp_dir=$(mktemp -d)
    trap "rm -rf ${tmp_dir}" EXIT
    
    # Determine archive format
    if [[ "$platform" == *"windows"* ]]; then
        archive_name="buildkite-ls_${version}_${platform}.zip"
    else
        archive_name="buildkite-ls_${version}_${platform}.tar.gz"
    fi
    
    download_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"
    
    info "Downloading from: ${download_url}"
    
    # Download archive
    if ! curl -L -o "${tmp_dir}/${archive_name}" "$download_url"; then
        error "Failed to download buildkite-ls"
    fi
    
    # Extract archive
    cd "$tmp_dir"
    if [[ "$archive_name" == *.zip ]]; then
        unzip -q "$archive_name"
    else
        tar -xzf "$archive_name"
    fi
    
    # Find binary (handle .exe extension on Windows)
    local binary_path
    if [[ "$platform" == *"windows"* ]]; then
        binary_path="${tmp_dir}/${BINARY_NAME}.exe"
    else
        binary_path="${tmp_dir}/${BINARY_NAME}"
    fi
    
    if [ ! -f "$binary_path" ]; then
        error "Binary not found in archive"
    fi
    
    # Make binary executable
    chmod +x "$binary_path"
    
    # Install binary
    info "Installing to ${INSTALL_DIR}/${BINARY_NAME}"
    
    if [ ! -w "$INSTALL_DIR" ]; then
        warn "Need sudo privileges to install to ${INSTALL_DIR}"
        sudo mv "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        mv "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
    fi
    
    success "buildkite-ls installed successfully!"
    
    # Verify installation
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        success "buildkite-ls is now available in your PATH"
        info "Version: $(buildkite-ls --version 2>/dev/null || echo 'Unable to determine version')"
    else
        warn "buildkite-ls installed but not found in PATH"
        warn "Make sure ${INSTALL_DIR} is in your PATH"
    fi
    
    info "Next steps:"
    info "1. Configure your editor to use buildkite-ls"
    info "2. See: https://github.com/${REPO}#editor-configuration"
}

# Main
main() {
    info "Buildkite Language Server Installer"
    info "Repository: https://github.com/${REPO}"
    
    # Check dependencies
    for cmd in curl tar; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            error "Required command not found: $cmd"
        fi
    done
    
    install_buildkite_ls
}

main "$@"