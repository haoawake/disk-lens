#!/usr/bin/env python3
"""生成一棵演示用的目录树（截图和自动测试用）。

    python3 scripts/make_demo_tree.py <目标文件夹> [每「GB」多少字节，默认 1 MB]

文件名、层级和大小比例模仿一台普通电脑：游戏、虚拟机、电影、照片、几千个缓存小文件……
大小按比例缩小（默认 1 GB → 1 MB），整棵树一百多 MB。文件内容写的是真实数据（不是稀疏文件），
这样在 macOS 上按「实际占用的磁盘块」统计时也有正确的大小。
"""
import os
import sys

root = sys.argv[1]
unit = int(sys.argv[2]) if len(sys.argv) > 2 else 1 << 20  # 1「GB」= 多少字节
os.makedirs(root, exist_ok=True)

chunk = b"\x5a" * (1 << 20)


def put(rel, gb):
    p = os.path.join(root, *rel.split("/"))
    os.makedirs(os.path.dirname(p), exist_ok=True)
    n = max(int(gb * unit), 1)
    with open(p, "wb") as f:
        while n > 0:
            k = min(n, len(chunk))
            f.write(chunk[:k])
            n -= k


put("游戏/原神/Data/资源包1.pak", 18)
put("游戏/原神/Data/资源包2.pak", 12)
put("游戏/原神/Data/音频/中文语音.pck", 9)
for i in range(60):
    put(f"游戏/原神/Data/StreamingAssets/block_{i:03d}.blk", 0.08)
put("游戏/原神/launcher.exe", 0.4)
put("代码/虚拟机/Ubuntu.vhdx", 24)
for i in range(300):
    put(f"代码/项目/web/node_modules/pkg{i:03d}/index.js", 0.002)
put("下载/电影/流浪地球2.mkv", 4.1)
put("下载/电影/星际穿越.mp4", 2.83)
put("下载/Windows 11.iso", 5.27)
put("下载/安装包/Photoshop_2025.dmg", 2.25)
put("下载/安装包/微信.dmg", 0.34)
put("下载/课程资料.rar", 1.56)
for y, n in (("2024", 14), ("2025", 11), ("2023", 7)):
    for i in range(n):
        put(f"照片/{y}/VID_{y}{i:04d}.mov", 0.42)
    for i in range(120):
        put(f"照片/{y}/IMG_{y}{i:04d}.heic", 0.0045)
for i in range(1500):
    put(f"缓存/浏览器缓存/Cache_Data/f_{i:06x}", 0.001)
put("缓存/日志/崩溃转储.dmp", 1.27)
for i in range(200):
    put(f"缓存/日志/app_{i:03d}.log", 0.0022)
for i in range(30):
    put(f"音乐/周杰伦/{i + 1:02d}.flac", 0.045)
put("音乐/季节.m4a", 0.12)
for i in range(25):
    put(f"文档/报告/第{i + 1}季度总结.pdf", 0.02)
put("文档/简历.docx", 0.003)
os.makedirs(os.path.join(root, "空文件夹"), exist_ok=True)
print("ok", root)
