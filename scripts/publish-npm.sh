#!/usr/bin/env bash
# publish-npm.sh — finalize 时从 GitHub Release 下载 5 平台 binary 组装进 npm 包，
# 然后提示手动 npm publish（含 5 binary，~7MB，OTP）。
#
# Usage: bash scripts/publish-npm.sh <tag>   (tag 如 v0.6.0)
# 前置: release.yml 的 goreleaser 已跑完、GitHub Release 已 publish（archive 可下载）。
# binary 落到 npm/bin/<platform>/（.gitignore 忽略，不进 git；npm publish 时含）。
set -euo pipefail

TAG="${1:?usage: publish-npm.sh <tag, e.g. v0.6.0>}"
VER="${TAG#v}"  # v0.6.0 → 0.6.0
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASE="https://github.com/0xByteBard404/readignore/releases/download/${TAG}"

# go (os, arch, ext) → npm 目录名
# 顺序: goos | goarch | ext | npm-dir
combos=(
  "linux amd64 tar.gz linux-x64"
  "linux arm64 tar.gz linux-arm64"
  "darwin amd64 tar.gz darwin-x64"
  "darwin arm64 tar.gz darwin-arm64"
  "windows amd64 zip windows-x64"
)

echo "组装 5 平台 binary 到 npm/bin/ ..."
rm -rf npm/bin
mkdir -p npm/bin

for line in "${combos[@]}"; do
  read -r goos goarch ext npmdir <<< "$line"
  archive="readignore_${VER}_${goos}_${goarch}.${ext}"
  url="${BASE}/${archive}"
  echo "  ${npmdir}: ${url}"
  curl -fsSL "$url" -o "/tmp/${archive}"
  mkdir -p "npm/bin/${npmdir}"
  if [ "$ext" = "zip" ]; then
    ( cd "npm/bin/${npmdir}" && unzip -o "/tmp/${archive}" readignore.exe && chmod +x readignore.exe 2>/dev/null || true )
  else
    ( cd "npm/bin/${npmdir}" && tar -xzf "/tmp/${archive}" readignore && chmod +x readignore )
  fi
  rm -f "/tmp/${archive}"
done

echo
echo "✓ 5 平台 binary 已组装到 npm/bin/："
ls -R npm/bin | head -20
echo
echo "现在手动发布（会提示 OTP）："
echo "  cd npm && npm publish"
