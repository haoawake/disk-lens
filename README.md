<p align="center"><img src="icon.png" width="88" alt="DiskLens icon"></p>

# DiskLens (文件清理助手)

Drive full again? Pick a drive and DiskLens scans it in seconds, drawing every folder and file as a block whose area matches its size. Double-click a folder to dive in, find what's actually eating your space, and right-click to send it to the Recycle Bin or delete it for good. Think SpaceSniffer: a single portable Windows app, no installation.

[中文说明](README.zh-CN.md) · [Download](https://github.com/haoawake/disk-lens/releases/latest)

![DiskLens: bigger blocks take more space; hover for size and path](docs/screenshot.png)

## Features

- **Fast.** 32 threads in parallel: a C: drive on NVMe with 1.63 million files and 230 thousand folders scans in about 3 seconds.
- **Live treemap.** Blocks grow while the scan runs; folders still being scanned are marked.
- **Readable.** Files are colored by type (video, images, archives/disk images, programs…); files too small to draw are merged into one hatched block. Three detail levels.
- **Drill down.** Double-click to zoom into a folder, jump back via the breadcrumb, `Backspace` for the parent, `Alt + ←` or the mouse back button to go back.
- **Native list.** An Explorer-style list on the right with system icons, sizes, share bars and file counts.
- **Delete in place.** Right-click → *Move to Recycle Bin* or *Delete permanently* (permanent deletion asks you to tick a confirmation box first). The view updates immediately.
- **Administrator mode.** One click relaunches DiskLens elevated: it then sees restore points and other users' folders, and permanent deletion also removes read-only files and files with restrictive permissions.
- **Safe by default.** Drive roots and core Windows folders (System32, WinSxS…) can't be deleted; program and AppData folders get a warning first. Junctions and symbolic links are removed as links — DiskLens never follows them into their targets.
- **Portable.** A single ~3 MB executable. No installer, no registry entries, no background service.

## Usage

1. Download `DiskLens-win-x64.exe` (or `DiskLens-win-arm64.exe` for ARM PCs) from [Releases](https://github.com/haoawake/disk-lens/releases/latest). Windows 10 or 11.
2. Run it. If SmartScreen says "Windows protected your PC", click *More info* → *Run anyway*.
3. Click a drive, pick a folder, paste a path, or drag a folder onto the window.
4. Hover a block for details, double-click to dive in, right-click for actions. `Delete` moves the selection to the Recycle Bin, `Shift + Delete` deletes it permanently.

Items in the Recycle Bin still occupy the drive until you empty it. If C: is critically full, use *Delete permanently*.

## Building

Requires Go 1.26+.

```bash
go test ./...
bash scripts/build.sh 1.0.0   # writes dist/DiskLens-win-x64.exe etc.
```

`go run .` also works for development (without the icon, DPI manifest and Common Controls v6, which the build script adds with go-winres). `go run . -bench C:\` scans without a window and prints the timing.

## License

[MIT](LICENSE)
