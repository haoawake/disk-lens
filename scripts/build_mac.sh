#!/usr/bin/env bash
# 构建 macOS 版：Apple 芯片和 Intel 两种都能用的通用程序，打包成「文件清理助手.app」，
# 再压成 dist/DiskLens-mac-universal.zip（里面只有这个 .app）。
#   用法：scripts/build_mac.sh 1.1.0
# 必须在 Mac 上运行（界面用到 AppKit，需要 cgo 和 Xcode 命令行工具）。
# 发版时由 .github/workflows/release.yml 在 macOS 的 runner 上调用。
#
# 资产名里带 mac-universal，工具库靠它给 Mac 访客挑对的安装包。
set -euo pipefail
cd "$(dirname "$0")/.."

VER="${1:-dev}"
APPNAME="文件清理助手"
EXE=DiskLens
BUNDLE_ID=com.haoawake.disk-lens
MIN_MACOS=12.0 # Go 1.26 本身最低要求 macOS 12
PLIST_VER="$VER"
[[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || PLIST_VER=0.0.0

OUT=dist/mac
rm -rf "$OUT" "dist/$EXE-mac-universal.zip"
mkdir -p "$OUT"

# 两种 CPU 各编一份，再用 lipo 合成一个
export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=$MIN_MACOS
for arch in arm64 amd64; do
  carch=$arch
  [ "$arch" = amd64 ] && carch=x86_64
  GOOS=darwin GOARCH=$arch CC="clang -arch $carch" \
    CGO_CFLAGS="-O2 -mmacosx-version-min=$MIN_MACOS" CGO_LDFLAGS="-mmacosx-version-min=$MIN_MACOS" \
    go build -trimpath -ldflags "-s -w -X main.version=$VER" -o "$OUT/$EXE-$arch" .
done

APP="$OUT/$APPNAME.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources/zh-Hans.lproj"
lipo -create -output "$APP/Contents/MacOS/$EXE" "$OUT/$EXE-arm64" "$OUT/$EXE-amd64"
rm "$OUT/$EXE-arm64" "$OUT/$EXE-amd64"

# 程序图标：assets/icon-mac.png（1024×1024，按 macOS 的图标规格留了边和阴影）缩成各种尺寸
ICONSET="$OUT/AppIcon.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z $s $s assets/icon-mac.png --out "$ICONSET/icon_${s}x${s}.png" > /dev/null
  sips -z $((s * 2)) $((s * 2)) assets/icon-mac.png --out "$ICONSET/icon_${s}x${s}@2x.png" > /dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"
rm -rf "$ICONSET"

# 界面只有中文：开发语言设成简体中文、带上 zh-Hans.lproj，
# 系统提供的菜单项、对话框按钮、「打开」面板也就都是中文
printf '"CFBundleDisplayName" = "%s";\n"CFBundleName" = "%s";\n' "$APPNAME" "$APPNAME" > "$APP/Contents/Resources/zh-Hans.lproj/InfoPlist.strings"
printf 'APPL????' > "$APP/Contents/PkgInfo"

# 读「桌面」「文稿」「下载」、移动硬盘、网络位置时系统会先问一句，下面这些是问的时候显示的理由
cat > "$APP/Contents/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>zh_CN</string>
	<key>CFBundleLocalizations</key>
	<array>
		<string>zh-Hans</string>
	</array>
	<key>CFBundleName</key>
	<string>$APPNAME</string>
	<key>CFBundleDisplayName</key>
	<string>$APPNAME</string>
	<key>CFBundleIdentifier</key>
	<string>$BUNDLE_ID</string>
	<key>CFBundleExecutable</key>
	<string>$EXE</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleShortVersionString</key>
	<string>$PLIST_VER</string>
	<key>CFBundleVersion</key>
	<string>$PLIST_VER</string>
	<key>LSMinimumSystemVersion</key>
	<string>$MIN_MACOS</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.utilities</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSPrincipalClass</key>
	<string>NSApplication</string>
	<key>NSSupportsAutomaticGraphicsSwitching</key>
	<true/>
	<key>NSHumanReadableCopyright</key>
	<string>© 2026 haoawake · MIT License</string>
	<key>NSDesktopFolderUsageDescription</key>
	<string>要统计「桌面」里的文件占了多少空间，需要读取这个文件夹。文件清理助手只看大小，不会上传任何东西。</string>
	<key>NSDocumentsFolderUsageDescription</key>
	<string>要统计「文稿」里的文件占了多少空间，需要读取这个文件夹。文件清理助手只看大小，不会上传任何东西。</string>
	<key>NSDownloadsFolderUsageDescription</key>
	<string>要统计「下载」里的文件占了多少空间，需要读取这个文件夹。文件清理助手只看大小，不会上传任何东西。</string>
	<key>NSRemovableVolumesUsageDescription</key>
	<string>要统计移动硬盘、U 盘上的文件占了多少空间，需要读取它们。</string>
	<key>NSNetworkVolumesUsageDescription</key>
	<string>要统计网络位置上的文件占了多少空间，需要读取它们。</string>
	<key>NSFileProviderDomainUsageDescription</key>
	<string>要统计 iCloud 云盘等云盘文件夹占了多少空间，需要读取它们。</string>
	<key>CFBundleDocumentTypes</key>
	<array>
		<dict>
			<key>CFBundleTypeName</key>
			<string>文件夹</string>
			<key>CFBundleTypeRole</key>
			<string>Viewer</string>
			<key>LSHandlerRank</key>
			<string>Alternate</string>
			<key>LSItemContentTypes</key>
			<array>
				<string>public.folder</string>
				<string>public.volume</string>
			</array>
		</dict>
	</array>
</dict>
</plist>
PLIST
plutil -lint "$APP/Contents/Info.plist"

# 没有开发者账号，只能「临时签名」（ad-hoc）。Apple 芯片的 Mac 不签名的程序根本不让运行
codesign --force --sign - --identifier "$BUNDLE_ID" "$APP"
codesign --verify --deep --strict --verbose=2 "$APP"

# ditto 而不是 zip：它会保留 .app 里的扩展属性和签名
(cd "$OUT" && ditto -c -k --sequesterRsrc --keepParent "$APPNAME.app" "../$EXE-mac-universal.zip")
ls -l dist
du -sh "$APP" "dist/$EXE-mac-universal.zip"
