#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS_DIR="$ROOT_DIR/.tools"
PLATFORM_DIR="$TOOLS_DIR/platform-tools"
ARCHIVE="$TOOLS_DIR/platform-tools-latest-darwin.zip"
URL="https://dl.google.com/android/repository/platform-tools-latest-darwin.zip"

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "错误：该辅助脚本仅用于 macOS。其他系统请从 Google Android Platform Tools 官方页面安装 adb。" >&2
    exit 1
fi

if [[ -x "$PLATFORM_DIR/adb" ]]; then
    echo "ADB 已存在：$PLATFORM_DIR/adb"
    "$PLATFORM_DIR/adb" version
    exit 0
fi

command -v curl >/dev/null 2>&1 || { echo "错误：未找到 curl" >&2; exit 1; }
command -v ditto >/dev/null 2>&1 || { echo "错误：未找到 ditto" >&2; exit 1; }

mkdir -p "$TOOLS_DIR"
echo "正在从 Google 官方地址下载 Android Platform Tools……"
curl --fail --location --progress-bar "$URL" --output "$ARCHIVE"
ditto -x -k "$ARCHIVE" "$TOOLS_DIR"
rm -f "$ARCHIVE"

if [[ ! -x "$PLATFORM_DIR/adb" ]]; then
    echo "错误：解压完成，但没有找到 $PLATFORM_DIR/adb" >&2
    exit 1
fi

echo "安装完成：$PLATFORM_DIR/adb"
"$PLATFORM_DIR/adb" version
