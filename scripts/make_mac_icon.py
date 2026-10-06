# 按 assets/icon.svg 的图案画 macOS 风格的图标 assets/icon-mac.png（1024×1024，主体 824×824 居中，带阴影）。
# 用法：python scripts/make_mac_icon.py（需要 Pillow 和 numpy）。图标改了才需要重新跑，结果提交到仓库里。
from PIL import Image, ImageDraw, ImageFilter
import numpy as np

S = 4  # 超采样
N = 1024 * S
k = 824 / 960
def X(v): return (100 + (v - 32) * k) * S

def lerp(a, b, t): return tuple(a[i] + (b[i] - a[i]) * t for i in range(3))
def hexc(h): return tuple(int(h[i:i+2], 16) for i in (1, 3, 5))

# 渐变背景（左上 → 右下）
c0, c1, c2 = hexc('#2fd6a6'), hexc('#10a8c4'), hexc('#2f6bff')
yy, xx = np.mgrid[0:N, 0:N].astype(np.float32)
x0, x1 = X(32), X(992)
t = ((xx - x0) + (yy - x0)) / (2 * (x1 - x0))
t = np.clip(t, 0, 1)
img = np.zeros((N, N, 3), np.float32)
for i in range(3):
    lo = c0[i] + (c1[i] - c0[i]) * np.clip(t / 0.5, 0, 1)
    hi = c1[i] + (c2[i] - c1[i]) * np.clip((t - 0.5) / 0.5, 0, 1)
    img[..., i] = np.where(t < 0.5, lo, hi)
grad = Image.fromarray(img.astype(np.uint8), 'RGB')

radius = 185 * S
body = Image.new('L', (N, N), 0)
ImageDraw.Draw(body).rounded_rectangle([X(32), X(32), X(992), X(992)], radius=radius, fill=255)

# 上方的高光（白色，从上往下渐隐，下边缘是一条弧线）
hl = Image.new('L', (N, N), 0)
d = ImageDraw.Draw(hl)
d.ellipse([X(32) - 600 * S, X(32) - 1200 * S, X(992) + 600 * S, X(400)], fill=255)
fade = np.clip(1 - (yy - X(32)) / (X(400) - X(32)) / 0.85, 0, 1) * 0.34
hl = Image.fromarray((np.array(hl, np.float32) / 255 * fade * 255).astype(np.uint8), 'L')

out = Image.new('RGBA', (N, N), (0, 0, 0, 0))
# 阴影
sh = Image.new('RGBA', (N, N), (0, 0, 0, 0))
sm = body.filter(ImageFilter.GaussianBlur(14 * S))
sh.putalpha(sm.point(lambda v: int(v * 0.32)))
out.alpha_composite(sh, (0, 10 * S))
layer = grad.convert('RGBA')
white = Image.new('RGBA', (N, N), (255, 255, 255, 255))
layer = Image.composite(white, layer, hl)
layer.putalpha(body)
out.alpha_composite(layer)

# 白色的方块（和 svg 一样的位置、透明度）
blocks = [(232, 232, 300, 560, 44, 1.0), (556, 232, 236, 290, 44, 0.86), (556, 546, 236, 110, 34, 0.68),
          (556, 680, 106, 112, 30, 0.52), (686, 680, 106, 112, 30, 0.38)]
for x, y, w, h, r, a in blocks:
    m = Image.new('L', (N, N), 0)
    ImageDraw.Draw(m).rounded_rectangle([X(x), X(y), X(x + w), X(y + h)], radius=r * k * S, fill=int(255 * a))
    wl = Image.new('RGBA', (N, N), (255, 255, 255, 0))
    wl.putalpha(m)
    out.alpha_composite(wl)

out = out.resize((1024, 1024), Image.LANCZOS)
out.save('assets/icon-mac.png', optimize=True)

print('ok')
