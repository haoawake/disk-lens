#!/usr/bin/env bash
# 构建 Windows 版发布包，放到 dist/。
#   用法：scripts/build.sh 1.0.0
# 发版时由 .github/workflows/release.yml 在 Ubuntu 上调用；本地用 Git Bash 也能跑。
#
# 资产名里带上系统和架构（win-x64、win-arm64），工具库靠它自动给访客挑对的安装包。
set -euo pipefail
cd "$(dirname "$0")/.."

VER="${1:-dev}"
NAME=DiskLens
rm -rf dist
mkdir -p dist

# 程序图标、「属性 → 详细信息」里的版本号，以及程序清单：
# 高分屏不发虚（PerMonitorV2），用新版系统控件（任务对话框、资源管理器风格的列表）
go install github.com/tc-hib/go-winres@v0.3.3
WINRES_VER="$VER"
[[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || WINRES_VER=0.0.0
"$(go env GOPATH)/bin/go-winres" simply \
  --icon winres/icon.png --manifest gui --arch amd64,arm64 \
  --product-name "文件清理助手" --file-description "文件清理助手 · 看看磁盘空间都去哪了" \
  --product-version "$WINRES_VER" --file-version "$WINRES_VER" \
  --copyright "© 2026 haoawake · MIT License" --original-filename "$NAME.exe"
trap 'rm -f rsrc_windows_*.syso' EXIT

# -H windowsgui：双击打开时不带黑色的命令行窗口
LDFLAGS="-s -w -H windowsgui -X main.version=$VER"
for arch in amd64 arm64; do
  label=$arch
  [ "$arch" = amd64 ] && label=x64
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "dist/$NAME-win-$label.exe" .
done

(cd dist && sha256sum -- * > SHA256SUMS.txt)
ls -l dist
