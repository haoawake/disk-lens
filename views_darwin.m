// Mac 版的各个视图：树图、起始页、右侧列表、底栏、提示条。
// 布局都在 isFlipped 的视图里手工排（y 向下），数字尽量和 Windows 版一致（Windows 的 1 像素 = 这里的 1 点）。

#import "views_darwin.h"
#import <QuartzCore/QuartzCore.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#include "_cgo_export.h"

DLRootView *dlRoot;
NSWindow *dlWindow;

// ---------------------------------------------------------------- 文字和形状

static NSParagraphStyle *paraStyle(NSTextAlignment align, NSLineBreakMode mode) {
	static NSMutableDictionary *cache;
	if (!cache) cache = [NSMutableDictionary dictionary];
	NSNumber *key = @((long)align * 100 + (long)mode);
	NSParagraphStyle *p = cache[key];
	if (!p) {
		NSMutableParagraphStyle *m = [[NSMutableParagraphStyle alloc] init];
		m.alignment = align;
		m.lineBreakMode = mode;
		p = m;
		cache[key] = p;
	}
	return p;
}

static CGFloat lineHeight(NSFont *f) { return ceil(f.ascender - f.descender + f.leading); }

void dlDrawLine(NSString *s, NSFont *f, NSColor *c, NSRect r, NSTextAlignment align, NSLineBreakMode mode) {
	if (s.length == 0 || r.size.width <= 1) return;
	NSDictionary *a = @{NSFontAttributeName : f, NSForegroundColorAttributeName : c, NSParagraphStyleAttributeName : paraStyle(align, mode)};
	CGFloat lh = lineHeight(f);
	NSRect lr = NSMakeRect(r.origin.x, r.origin.y + floor((r.size.height - lh) / 2), r.size.width, lh);
	[s drawWithRect:lr options:NSStringDrawingUsesLineFragmentOrigin | NSStringDrawingTruncatesLastVisibleLine attributes:a context:nil];
}

void dlDrawWrapped(NSString *s, NSFont *f, NSColor *c, NSRect r) {
	if (s.length == 0 || r.size.width <= 1) return;
	NSDictionary *a = @{NSFontAttributeName : f, NSForegroundColorAttributeName : c, NSParagraphStyleAttributeName : paraStyle(NSTextAlignmentLeft, NSLineBreakByWordWrapping)};
	[s drawWithRect:r options:NSStringDrawingUsesLineFragmentOrigin | NSStringDrawingTruncatesLastVisibleLine attributes:a context:nil];
}

CGFloat dlTextWidth(NSString *s, NSFont *f) {
	if (s.length == 0) return 0;
	return ceil([s sizeWithAttributes:@{NSFontAttributeName : f}].width);
}

CGFloat dlTextHeight(NSString *s, NSFont *f, CGFloat width) {
	if (s.length == 0) return 0;
	NSDictionary *a = @{NSFontAttributeName : f, NSParagraphStyleAttributeName : paraStyle(NSTextAlignmentLeft, NSLineBreakByWordWrapping)};
	return ceil([s boundingRectWithSize:NSMakeSize(width, CGFLOAT_MAX) options:NSStringDrawingUsesLineFragmentOrigin attributes:a context:nil].size.height);
}

void dlFillRound(NSRect r, CGFloat radius, NSColor *c) {
	if (r.size.width <= 0 || r.size.height <= 0) return;
	radius = MIN(radius, MIN(r.size.width, r.size.height) / 2);
	[c setFill];
	[[NSBezierPath bezierPathWithRoundedRect:r xRadius:radius yRadius:radius] fill];
}

void dlRoundBox(NSRect r, CGFloat radius, NSColor *bg, NSColor *border) {
	dlFillRound(r, radius, border);
	dlFillRound(NSInsetRect(r, 1, 1), radius - 1, bg);
}

static NSFont *font(CGFloat size, bool bold) {
	return bold ? [NSFont boldSystemFontOfSize:size] : [NSFont systemFontOfSize:size];
}

// ---------------------------------------------------------------- 树图

// 树图的配色，和 Windows 版的 mapStyle 一样
enum {
	M_BG = 0xE3E7EE,
	M_DIR_BORDER = 0xC3CAD6,
	M_DIR_HEADER = 0xDCE2EB,
	M_DIR_HEADER2 = 0xE6EAF1,
	M_DIR_FILL = 0xEDF0F5,
	M_DIR_TEXT = 0x232A38,
	M_DIR_TEXT2 = 0x667080,
	M_TILE_TEXT = 0x1A202C,
	M_TILE_TEXT2 = 0x485264,
	M_REST = 0xDCE1E8,
	M_REST_LINE = 0xC5CCD7,
	M_SCANNING = 0x0A6CFF,
};

static uint32_t mix(uint32_t c, uint32_t d, double t) {
	uint32_t out = 0;
	for (int sh = 16; sh >= 0; sh -= 8) {
		double a = (c >> sh) & 0xFF, b = (d >> sh) & 0xFF;
		out |= ((uint32_t)lround(a + (b - a) * t) & 0xFF) << sh;
	}
	return out;
}

static void cgFill(CGContextRef c, CGRect r, uint32_t col) {
	if (r.size.width <= 0 || r.size.height <= 0) return;
	CGContextSetRGBFillColor(c, ((col >> 16) & 0xFF) / 255.0, ((col >> 8) & 0xFF) / 255.0, (col & 0xFF) / 255.0, 1);
	CGContextFillRect(c, r);
}

static void cgGradientV(CGContextRef c, CGColorSpaceRef cs, CGRect r, uint32_t top, uint32_t bottom) {
	CGFloat comps[8] = {
		((top >> 16) & 0xFF) / 255.0, ((top >> 8) & 0xFF) / 255.0, (top & 0xFF) / 255.0, 1,
		((bottom >> 16) & 0xFF) / 255.0, ((bottom >> 8) & 0xFF) / 255.0, (bottom & 0xFF) / 255.0, 1,
	};
	CGGradientRef g = CGGradientCreateWithColorComponents(cs, comps, NULL, 2);
	CGContextSaveGState(c);
	CGContextClipToRect(c, r);
	CGContextDrawLinearGradient(c, g, CGPointMake(0, r.origin.y), CGPointMake(0, r.origin.y + r.size.height), 0);
	CGContextRestoreGState(c);
	CGGradientRelease(g);
}

static CGRect R(int32_t x0, int32_t y0, int32_t x1, int32_t y1) { return CGRectMake(x0, y0, x1 - x0, y1 - y0); }

@implementation DLMapView {
	DLTile *_tiles;
	int _n;
	char *_text;
	bool _scanning;
	double _imgScale;
	CGImageRef _image, _old;
	NSImage *_nsImage, *_nsOld;
	bool _hasSel, _hasHover;
	NSRect _sel, _hover; // 像素
	NSString *_tipName, *_tipSize, *_tipShare, *_tipLines, *_tipPath, *_tipHint;
	NSPoint _mouse; // 点，视图坐标
	NSString *_message;
	NSTimer *_animTimer;
	CFTimeInterval _animStart;
	NSRect _animFrom, _animTo;
	bool _animOld;
	NSTrackingArea *_track;
}

- (BOOL)isFlipped { return YES; }
- (BOOL)acceptsFirstResponder { return YES; }
- (BOOL)acceptsFirstMouse:(NSEvent *)e { return YES; }
- (BOOL)isOpaque { return YES; }

- (double)scale {
	double s = self.window ? self.window.backingScaleFactor : 2;
	if (self.forcedScale > s) s = self.forcedScale;
	return s;
}

- (void)pixelWidth:(int32_t *)w height:(int32_t *)h scale:(double *)scale {
	double s = [self scale];
	*w = (int32_t)llround(self.bounds.size.width * s);
	*h = (int32_t)llround(self.bounds.size.height * s);
	*scale = s;
}

- (void)setFrameSize:(NSSize)size {
	BOOL changed = !NSEqualSizes(size, self.frame.size);
	[super setFrameSize:size];
	if (changed) goMapResized();
}

- (void)viewDidChangeBackingProperties {
	[super viewDidChangeBackingProperties];
	goMapResized();
}

- (void)dealloc {
	free(_tiles);
	free(_text);
	if (_image) CGImageRelease(_image);
	if (_old) CGImageRelease(_old);
}

- (void)setTiles:(DLTile *)tiles count:(int)n text:(char *)text scanning:(bool)scanning scale:(double)scale {
	free(_tiles);
	free(_text);
	_tiles = tiles;
	_n = n;
	_text = text;
	_scanning = scanning;
	_imgScale = scale;
	[self render];
	[self setNeedsDisplay:YES];
}

- (NSString *)str:(int32_t)off len:(int32_t)len {
	if (len <= 0) return @"";
	NSString *s = [[NSString alloc] initWithBytes:_text + off length:len encoding:NSUTF8StringEncoding];
	return s ? s : @"";
}

// render 把所有方块画到一张位图上（像素坐标，y 向下），之后悬停、选中只在上面叠加
- (void)render {
	double s = _imgScale > 0 ? _imgScale : [self scale];
	size_t W = (size_t)llround(self.bounds.size.width * s), H = (size_t)llround(self.bounds.size.height * s);
	if (W < 1 || H < 1) return;
	CGColorSpaceRef cs = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
	CGContextRef c = CGBitmapContextCreate(NULL, W, H, 8, 0, cs, kCGImageAlphaPremultipliedFirst | kCGBitmapByteOrder32Little);
	if (!c) {
		CGColorSpaceRelease(cs);
		return;
	}
	CGContextTranslateCTM(c, 0, H);
	CGContextScaleCTM(c, 1, -1);
	NSGraphicsContext *g = [NSGraphicsContext graphicsContextWithCGContext:c flipped:YES];
	[NSGraphicsContext saveGraphicsState];
	[NSGraphicsContext setCurrentContext:g];

	cgFill(c, CGRectMake(0, 0, W, H), M_BG);
	NSFont *f = font(12 * s, false), *fb = font(12 * s, true);
	NSColor *dirText = dlColor(M_DIR_TEXT), *dirText2 = dlColor(M_DIR_TEXT2);
	NSColor *tileText = dlColor(M_TILE_TEXT), *tileText2 = dlColor(M_TILE_TEXT2), *scanning = dlColor(M_SCANNING);
	for (int i = 0; i < _n; i++) {
		DLTile *t = &_tiles[i];
		CGRect r = R(t->x0, t->y0, t->x1, t->y1);
		CGRect in = R(t->x0, t->y0, t->x1 - 1, t->y1 - 1);
		switch (t->kind) {
		case 1: // 文件
			if (in.size.width <= 0 || in.size.height <= 0) {
				cgFill(c, r, t->color);
				break;
			}
			if (in.size.width >= 6 && in.size.height >= 6) {
				cgGradientV(c, cs, in, mix(t->color, 0xFFFFFF, 0.3), mix(t->color, 0x000000, 0.06));
			} else {
				cgFill(c, in, t->color);
			}
			[self label:t rect:in font:f c1:tileText c2:tileText2 scale:s];
			break;
		case 2: // 一堆小文件：斜线纹理，一眼看出这块是「一堆小东西」
			cgFill(c, in, M_REST);
			if (in.size.width > 0 && in.size.height > 0) {
				CGContextSaveGState(c);
				CGContextClipToRect(c, in);
				CGContextSetRGBStrokeColor(c, 0xC5 / 255.0, 0xCC / 255.0, 0xD7 / 255.0, 1);
				CGContextSetLineWidth(c, MAX(1, round(s)));
				CGFloat step = round(6 * s);
				for (CGFloat x = in.origin.x - in.size.height; x < CGRectGetMaxX(in); x += step) {
					CGContextMoveToPoint(c, x, CGRectGetMaxY(in));
					CGContextAddLineToPoint(c, x + in.size.height, in.origin.y);
				}
				CGContextStrokePath(c);
				CGContextRestoreGState(c);
			}
			[self label:t rect:in font:f c1:dirText2 c2:dirText2 scale:s];
			break;
		default: { // 文件夹
			cgFill(c, r, M_DIR_BORDER);
			cgFill(c, in, (t->flags & DL_TILE_EVEN) ? M_DIR_HEADER2 : M_DIR_HEADER);
			CGRect inner = R(t->ix0, t->iy0, t->ix1, t->iy1);
			if (t->flags & DL_TILE_HEADER) {
				NSRect hr = NSMakeRect(in.origin.x + round(4 * s), in.origin.y, in.size.width - 2 * round(4 * s), t->iy0 - t->y0);
				NSString *name = [self str:t->name len:t->nameLen], *size = [self str:t->size len:t->sizeLen];
				CGFloat sw = dlTextWidth(size, f), nw = dlTextWidth(name, fb);
				if (hr.size.width > nw + sw + 10 * s) {
					dlDrawLine(name, fb, dirText, NSMakeRect(hr.origin.x, hr.origin.y, hr.size.width - sw - 6 * s, hr.size.height), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
					dlDrawLine(size, f, (t->flags & DL_TILE_SCANNING) ? scanning : dirText2,
					           NSMakeRect(NSMaxX(hr) - sw, hr.origin.y, sw, hr.size.height), NSTextAlignmentLeft, NSLineBreakByClipping);
				} else {
					dlDrawLine(name, fb, dirText, hr, NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
				}
			}
			cgFill(c, inner, M_DIR_FILL);
			if (!(t->flags & DL_TILE_KIDS) && !(t->flags & DL_TILE_HEADER)) {
				[self label:t rect:inner font:f c1:dirText c2:dirText2 scale:s];
			}
		}
		}
	}

	[NSGraphicsContext restoreGraphicsState];
	if (_image) CGImageRelease(_image);
	_image = CGBitmapContextCreateImage(c);
	CGContextRelease(c);
	CGColorSpaceRelease(cs);
	_nsImage = _image ? [[NSImage alloc] initWithCGImage:_image size:NSMakeSize(W / s, H / s)] : nil;
}

// label 在方块里写名字和大小，地方不够就不写
- (void)label:(DLTile *)t rect:(CGRect)r font:(NSFont *)f c1:(NSColor *)c1 c2:(NSColor *)c2 scale:(double)s {
	if (r.size.width < 34 * s || r.size.height < 15 * s) return;
	CGFloat pad = round(4 * s), lh = round(16 * s);
	NSRect in = NSMakeRect(r.origin.x + pad, r.origin.y + round(2 * s), r.size.width - 2 * pad, r.size.height);
	NSString *name = [self str:t->name len:t->nameLen];
	dlDrawLine(name, f, c1, NSMakeRect(in.origin.x, in.origin.y, in.size.width, lh), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
	if (r.size.height >= 2 * lh + 4 * s) {
		NSString *size = [self str:t->size len:t->sizeLen];
		dlDrawLine(size, f, c2, NSMakeRect(in.origin.x, in.origin.y + lh, in.size.width, lh), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
	}
}

- (NSRect)toPoints:(NSRect)px {
	double s = _imgScale > 0 ? _imgScale : [self scale];
	return NSMakeRect(px.origin.x / s, px.origin.y / s, px.size.width / s, px.size.height / s);
}

- (void)setMarksSel:(bool)sel rect:(NSRect)sr hover:(bool)hover rect:(NSRect)hr {
	_hasSel = sel;
	_sel = sr;
	_hasHover = hover;
	_hover = hr;
	[self setNeedsDisplay:YES];
}

- (void)setTipName:(NSString *)name size:(NSString *)size share:(NSString *)share lines:(NSString *)lines path:(NSString *)path hint:(NSString *)hint {
	_tipName = name;
	_tipSize = size;
	_tipShare = share;
	_tipLines = lines;
	_tipPath = path;
	_tipHint = hint;
	[self setNeedsDisplay:YES];
}

- (void)setMessage:(NSString *)msg {
	_message = msg.length ? msg : nil;
	[self setNeedsDisplay:YES];
}

// ---- 钻进去、退出来的动画：在旧图（或新图）上把一块区域放大到整个画面

- (void)zoomFrom:(NSRect)from to:(NSRect)to useOld:(bool)useOld {
	if (!_image) return;
	if (useOld) {
		if (_old) CGImageRelease(_old);
		_old = CGImageRetain(_image);
		_nsOld = _nsImage;
	}
	_animFrom = from;
	_animTo = to;
	_animOld = useOld;
	_animStart = CACurrentMediaTime();
	[_animTimer invalidate];
	_animTimer = [NSTimer timerWithTimeInterval:1.0 / 60 target:self selector:@selector(animTick:) userInfo:nil repeats:YES];
	[[NSRunLoop currentRunLoop] addTimer:_animTimer forMode:NSRunLoopCommonModes];
	_hasHover = false;
	_tipName = nil;
	[self setNeedsDisplay:YES];
}

- (void)animTick:(NSTimer *)t {
	if (CACurrentMediaTime() - _animStart >= 0.17) {
		[_animTimer invalidate];
		_animTimer = nil;
		if (_old) CGImageRelease(_old);
		_old = NULL;
		_nsOld = nil;
		goMapAnimDone();
	}
	[self setNeedsDisplay:YES];
}

- (BOOL)animating { return _animTimer != nil; }

// ---- 画到屏幕上

- (void)drawRect:(NSRect)dirty {
	[dlColor(M_BG) setFill];
	NSRectFill(NSIntersectionRect(dirty, self.bounds)); // macOS 14 起 dirty 可能超出自己的范围，填满它会盖住旁边的视图
	NSRect b = self.bounds;
	if (_animTimer) {
		NSImage *img = _animOld ? _nsOld : _nsImage;
		CGImageRef cg = _animOld ? _old : _image;
		if (img && cg) {
			double t = (CACurrentMediaTime() - _animStart) / 0.17;
			t = MIN(MAX(t, 0), 1);
			t = 1 - (1 - t) * (1 - t) * (1 - t);
			NSRect a = _animFrom, z = _animTo;
			NSRect src = NSMakeRect(a.origin.x + (z.origin.x - a.origin.x) * t, a.origin.y + (z.origin.y - a.origin.y) * t,
			                        a.size.width + (z.size.width - a.size.width) * t, a.size.height + (z.size.height - a.size.height) * t);
			double s = (double)CGImageGetWidth(cg) / img.size.width;
			double H = CGImageGetHeight(cg);
			// NSImage 的 fromRect 用的是图片自己的坐标（左下角为原点，单位是点）
			NSRect from = NSMakeRect(src.origin.x / s, (H - src.origin.y - src.size.height) / s, MAX(src.size.width, 1) / s, MAX(src.size.height, 1) / s);
			[NSGraphicsContext currentContext].imageInterpolation = NSImageInterpolationHigh;
			[img drawInRect:b fromRect:from operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
		}
		return;
	}
	if (_nsImage) {
		[_nsImage drawInRect:NSMakeRect(0, 0, _nsImage.size.width, _nsImage.size.height) fromRect:NSZeroRect
		           operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
	}
	if (_message) {
		dlDrawLine(_message, font(13, false), dlColor(C_TEXT3), b, NSTextAlignmentCenter, NSLineBreakByTruncatingTail);
	}
	if (_hasSel) {
		NSRect r = [self toPoints:_sel];
		[dlColor(0x15171C) setStroke];
		NSBezierPath *p = [NSBezierPath bezierPathWithRect:NSInsetRect(r, 1, 1)];
		p.lineWidth = 2;
		[p stroke];
		[[NSColor whiteColor] setStroke];
		p = [NSBezierPath bezierPathWithRect:NSInsetRect(r, 2.5, 2.5)];
		p.lineWidth = 1;
		[p stroke];
	}
	if (_hasHover) {
		NSRect r = [self toPoints:_hover];
		[dlColor(C_ACCENT) setStroke];
		NSBezierPath *p = [NSBezierPath bezierPathWithRect:NSInsetRect(r, 1, 1)];
		p.lineWidth = 2;
		[p stroke];
		if (_tipName) [self drawTip];
	}
}

// drawTip 画鼠标旁边的提示框：名字、大小、占比、类型、完整路径、操作提示
- (void)drawTip {
	NSFont *fName = font(13, true), *fBig = font(24, true), *fSmall = font(12, false);
	CGFloat pad = 12, maxW = 380;
	NSArray<NSString *> *lines = _tipLines.length ? [_tipLines componentsSeparatedByString:@"\n"] : @[];
	CGFloat w = MAX(MIN(dlTextWidth(_tipName, fName), maxW), dlTextWidth(_tipSize, fBig) + 10 + dlTextWidth(_tipShare, fSmall));
	for (NSString *l in lines) w = MAX(w, dlTextWidth(l, fSmall));
	w = MIN(MAX(w, 180), maxW);
	CGFloat pathH = _tipPath.length ? MIN(dlTextHeight(_tipPath, fSmall, w), 54) : 0;
	CGFloat h = pad * 2 + 20 + 32 + lines.count * 18 + 4 + pathH + 22;
	w += pad * 2;

	NSRect b = self.bounds;
	CGFloat x = _mouse.x + 16, y = _mouse.y + 18;
	if (x + w > NSMaxX(b) - 4) x = _mouse.x - w - 12;
	if (y + h > NSMaxY(b) - 4) y = _mouse.y - h - 12;
	x = MAX(x, 4);
	y = MAX(y, 4);
	NSRect box = NSMakeRect(x, y, w, h);

	[NSGraphicsContext saveGraphicsState];
	NSShadow *sh = [[NSShadow alloc] init];
	sh.shadowColor = [NSColor colorWithWhite:0 alpha:0.16];
	sh.shadowBlurRadius = 10;
	sh.shadowOffset = NSMakeSize(0, -3);
	[sh set];
	dlFillRound(box, 12, [NSColor whiteColor]);
	[NSGraphicsContext restoreGraphicsState];
	[dlColor(0xDADFE7) setStroke];
	NSBezierPath *border = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(box, 0.5, 0.5) xRadius:12 yRadius:12];
	border.lineWidth = 1;
	[border stroke];

	CGFloat cx = x + pad, cy = y + pad, cw = w - 2 * pad;
	dlDrawLine(_tipName, fName, dlColor(C_TEXT), NSMakeRect(cx, cy, cw, 20), NSTextAlignmentLeft, NSLineBreakByTruncatingMiddle);
	cy += 20;
	CGFloat sw = dlTextWidth(_tipSize, fBig);
	dlDrawLine(_tipSize, fBig, dlColor(C_TEXT), NSMakeRect(cx, cy, sw + 2, 32), NSTextAlignmentLeft, NSLineBreakByClipping);
	dlDrawLine(_tipShare, fSmall, dlColor(C_TEXT2), NSMakeRect(cx + sw + 10, cy + 6, cw - sw - 10, 24), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
	cy += 32;
	for (NSString *l in lines) {
		dlDrawLine(l, fSmall, dlColor(C_TEXT2), NSMakeRect(cx, cy, cw, 18), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
		cy += 18;
	}
	cy += 4;
	if (pathH > 0) {
		dlDrawWrapped(_tipPath, fSmall, dlColor(C_TEXT3), NSMakeRect(cx, cy, cw, pathH));
		cy += pathH;
	}
	dlDrawLine(_tipHint, fSmall, dlColor(C_ACCENT), NSMakeRect(cx, cy, cw, 22), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
}

// ---- 鼠标键盘

- (void)updateTrackingAreas {
	[super updateTrackingAreas];
	if (_track) [self removeTrackingArea:_track];
	_track = [[NSTrackingArea alloc] initWithRect:NSZeroRect
	                                      options:NSTrackingMouseMoved | NSTrackingMouseEnteredAndExited | NSTrackingActiveInKeyWindow | NSTrackingInVisibleRect
	                                        owner:self
	                                     userInfo:nil];
	[self addTrackingArea:_track];
}

- (NSPoint)pixels:(NSPoint)p {
	double s = _imgScale > 0 ? _imgScale : [self scale];
	return NSMakePoint(p.x * s, p.y * s);
}

- (void)mouseMoved:(NSEvent *)e {
	_mouse = [self convertPoint:e.locationInWindow fromView:nil];
	if (!_animTimer) {
		NSPoint p = [self pixels:_mouse];
		goMapMouse(DL_MOUSE_MOVE, p.x, p.y, 0);
	}
	if (_tipName) [self setNeedsDisplay:YES];
}

- (void)mouseEntered:(NSEvent *)e { [self mouseMoved:e]; }

- (void)mouseExited:(NSEvent *)e { goMapMouse(DL_MOUSE_EXIT, 0, 0, 0); }

- (void)mouseDown:(NSEvent *)e {
	[self.window makeFirstResponder:self];
	_mouse = [self convertPoint:e.locationInWindow fromView:nil];
	NSPoint p = [self pixels:_mouse];
	if (e.modifierFlags & NSEventModifierFlagControl) {
		goMapMouse(DL_MOUSE_RIGHT, p.x, p.y, 1);
		return;
	}
	goMapMouse(DL_MOUSE_DOWN, p.x, p.y, (int)e.clickCount);
}

- (void)rightMouseDown:(NSEvent *)e {
	[self.window makeFirstResponder:self];
	_mouse = [self convertPoint:e.locationInWindow fromView:nil];
	NSPoint p = [self pixels:_mouse];
	goMapMouse(DL_MOUSE_RIGHT, p.x, p.y, 1);
}

- (void)otherMouseDown:(NSEvent *)e {
	if (e.buttonNumber == 3) {
		goMapMouse(DL_MOUSE_BACK, 0, 0, 0);
	} else if (e.buttonNumber == 4) {
		goMapMouse(DL_MOUSE_FORWARD, 0, 0, 0);
	} else {
		[super otherMouseDown:e];
	}
}

// 截图模式里模拟鼠标位置（点）
- (void)setMouseForShot:(NSPoint)p { _mouse = p; }

- (void)keyDown:(NSEvent *)e {
	if (!dlHandleKey(e)) [super keyDown:e];
}

- (void)copy:(id)sender { goCommand(DL_CMD_COPY_PATH); }

@end

// dlHandleKey 处理树图和列表共用的按键，处理了返回 YES
BOOL dlHandleKey(NSEvent *e) {
	NSEventModifierFlags m = e.modifierFlags & (NSEventModifierFlagCommand | NSEventModifierFlagOption | NSEventModifierFlagControl | NSEventModifierFlagShift);
	switch (e.keyCode) {
	case 51: // ⌫：上一级（⌘⌫、⌥⌘⌫ 由菜单处理）
		if (m == 0) {
			goCommand(DL_CMD_UP);
			return YES;
		}
		break;
	case 117: // ⌦：和 Windows 的 Delete 一样，移到废纸篓（会先确认）
		if (m == 0) {
			goCommand(DL_CMD_TRASH);
			return YES;
		}
		break;
	case 36:
	case 76: // 回车：打开
		if (m == 0) {
			goCommand(DL_CMD_OPEN);
			return YES;
		}
		break;
	case 53: // Esc：取消选中
		goCommand(DL_CMD_DESELECT);
		return YES;
	}
	return NO;
}

// ---------------------------------------------------------------- 起始页

@interface DLCardView : NSView
@property(nonatomic) int index;
@property(nonatomic, copy) NSString *path, *title, *badge, *sub;
@property(nonatomic) double used;
@property(nonatomic) bool low, hot;
@property(nonatomic, strong) NSImage *icon;
@end

@implementation DLCardView {
	NSTrackingArea *_track;
}
- (BOOL)isFlipped { return YES; }
- (BOOL)acceptsFirstMouse:(NSEvent *)e { return YES; }

- (void)updateTrackingAreas {
	[super updateTrackingAreas];
	if (_track) [self removeTrackingArea:_track];
	_track = [[NSTrackingArea alloc] initWithRect:NSZeroRect options:NSTrackingMouseEnteredAndExited | NSTrackingActiveInActiveApp | NSTrackingInVisibleRect owner:self userInfo:nil];
	[self addTrackingArea:_track];
}
- (void)resetCursorRects { [self addCursorRect:self.bounds cursor:[NSCursor pointingHandCursor]]; }
- (void)mouseEntered:(NSEvent *)e {
	self.hot = true;
	[self setNeedsDisplay:YES];
}
- (void)mouseExited:(NSEvent *)e {
	self.hot = false;
	[self setNeedsDisplay:YES];
}
- (void)mouseDown:(NSEvent *)e {}
- (void)mouseUp:(NSEvent *)e {
	if (NSPointInRect([self convertPoint:e.locationInWindow fromView:nil], self.bounds)) goCard(self.index);
}

- (void)drawRect:(NSRect)dirty {
	NSRect r = self.bounds;
	dlRoundBox(r, 14, self.hot ? dlColor(0xFAFCFF) : [NSColor whiteColor], self.hot ? dlColor(0xB9CCF0) : dlColor(0xE1E5EC));
	CGFloat pad = 16, is = 48;
	NSRect ib = NSMakeRect(pad, floor((r.size.height - is) / 2), is, is);
	[self.icon drawInRect:ib fromRect:NSZeroRect operation:NSCompositingOperationSourceOver fraction:1 respectFlipped:YES hints:nil];

	CGFloat x = NSMaxX(ib) + 14, right = r.size.width - pad, y = 16;
	NSFont *fb = font(13, true), *fs = font(12, false);
	CGFloat tw = dlTextWidth(self.title, fb);
	CGFloat bw = self.badge.length ? dlTextWidth(self.badge, font(11, false)) + 12 : 0;
	CGFloat tr = MIN(x + tw + 2, right - bw - 6);
	dlDrawLine(self.title, fb, dlColor(C_TEXT), NSMakeRect(x, y, tr - x, 20), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
	if (bw > 0) {
		NSRect br = NSMakeRect(tr + 6, y + 1, bw, 18);
		dlFillRound(br, 5, dlColor(C_SOFT));
		dlDrawLine(self.badge, font(11, false), dlColor(C_TEXT2), br, NSTextAlignmentCenter, NSLineBreakByClipping);
	}
	y += 28;
	NSRect bar = NSMakeRect(x, y, right - x, 7);
	dlFillRound(bar, 3.5, dlColor(0xE6E9EF));
	CGFloat uw = floor(bar.size.width * MIN(MAX(self.used, 0), 1));
	if (uw > 0) dlFillRound(NSMakeRect(x, y, MAX(uw, 7), 7), 3.5, self.low ? dlColor(C_DANGER) : dlColor(C_ACCENT));
	y += 15;
	dlDrawLine(self.sub, fs, self.low ? dlColor(C_DANGER) : dlColor(C_TEXT2), NSMakeRect(x, y, right - x, 18), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
}
@end

// DLBox 是一块圆角的浅色底
@interface DLBox : NSView
@property(nonatomic, strong) NSColor *fill;
@property(nonatomic) CGFloat radius;
@end
@implementation DLBox
- (BOOL)isFlipped { return YES; }
- (void)drawRect:(NSRect)dirty { dlFillRound(self.bounds, self.radius, self.fill); }
@end

static NSTextField *label(NSString *s, NSFont *f, NSColor *c) {
	NSTextField *t = [NSTextField labelWithString:s];
	t.font = f;
	t.textColor = c;
	t.lineBreakMode = NSLineBreakByTruncatingTail;
	return t;
}

static NSButton *button(NSString *title, NSString *symbol, int cmd, id target) {
	NSButton *b = [NSButton buttonWithTitle:title target:target action:@selector(buttonCommand:)];
	b.tag = cmd;
	b.bezelStyle = NSBezelStyleRounded;
	b.controlSize = NSControlSizeLarge;
	b.font = font(13, false);
	if (symbol) {
		b.image = [NSImage imageWithSystemSymbolName:symbol accessibilityDescription:nil];
		b.imagePosition = NSImageLeading;
	}
	return b;
}

@implementation DLHomeView {
	NSTextField *_title, *_sub, *_loading, *_hint, *_tipTitle, *_tipText;
	NSButton *_pick, *_scan, *_tipButton;
	DLBox *_tip;
	NSMutableArray<DLCardView *> *_cards;
	bool _showTip, _isLoading;
}

- (instancetype)initWithFrame:(NSRect)f {
	self = [super initWithFrame:f];
	_cards = [NSMutableArray array];
	_title = label(@"选一个磁盘，看看空间都去哪了", font(24, true), dlColor(C_TEXT));
	_sub = label(@"扫描时边扫边画：方块越大，占的空间越多。双击文件夹钻进去，右键可以打开、在访达中显示或者删除。", font(13, false), dlColor(C_TEXT2));
	_loading = label(@"正在读取磁盘…", font(13, false), dlColor(C_TEXT3));
	_hint = label(@"提示：也可以把文件夹直接拖进这个窗口，或者拖到程序坞里的「文件清理助手」图标上。", font(13, false), dlColor(C_TEXT3));
	_pick = button(@"选择文件夹…", @"folder", DL_CMD_PICK, self);
	_pick.bezelColor = dlColor(C_ACCENT);
	_pick.contentTintColor = [NSColor whiteColor];
	_scan = button(@"扫描", nil, 0, self);
	_scan.action = @selector(scanTyped:);
	_pathField = [[NSTextField alloc] initWithFrame:NSZeroRect];
	_pathField.placeholderString = @"或者粘贴一个文件夹路径，比如 ~/Downloads";
	_pathField.font = font(14, false);
	_pathField.bezelStyle = NSTextFieldRoundedBezel;
	_pathField.controlSize = NSControlSizeLarge;
	_pathField.target = self;
	_pathField.action = @selector(scanTyped:);
	_pathField.cell.scrollable = YES;
	_pathField.cell.wraps = NO;

	_tip = [[DLBox alloc] initWithFrame:NSZeroRect];
	_tip.fill = dlColor(0xEAEEF4);
	_tip.radius = 12;
	_tipTitle = label(@"想统计得更完整？", font(13, true), dlColor(C_TEXT));
	_tipText = [NSTextField wrappingLabelWithString:@"给文件清理助手「完全磁盘访问权限」：能读到邮件、信息、废纸篓和其他应用的数据。在「系统设置 → 隐私与安全性」里打开。"];
	_tipText.font = font(13, false);
	_tipText.textColor = dlColor(C_TEXT2);
	_tipButton = button(@"打开系统设置…", @"lock.shield", DL_CMD_FDA, self);
	[_tip addSubview:_tipTitle];
	[_tip addSubview:_tipText];
	[_tip addSubview:_tipButton];

	for (NSView *v in @[ _title, _sub, _loading, _pick, _pathField, _scan, _tip, _hint ]) [self addSubview:v];
	_isLoading = true;
	return self;
}

- (BOOL)isFlipped { return YES; }

- (void)drawRect:(NSRect)dirty {
	[dlColor(C_BG) setFill];
	NSRectFill(NSIntersectionRect(dirty, self.bounds)); // macOS 14 起 dirty 可能超出自己的范围，填满它会盖住旁边的视图
}

- (void)buttonCommand:(NSButton *)b { goCommand((int)b.tag); }

- (void)scanTyped:(id)sender {
	NSString *p = [self.pathField.stringValue stringByTrimmingCharactersInSet:[NSCharacterSet whitespaceAndNewlineCharacterSet]];
	goOpenPath((char *)p.UTF8String);
}

- (void)setCards:(DLCard *)cards count:(int)n loading:(bool)loading {
	for (DLCardView *c in _cards) [c removeFromSuperview];
	[_cards removeAllObjects];
	for (int i = 0; i < n; i++) {
		DLCardView *c = [[DLCardView alloc] initWithFrame:NSZeroRect];
		c.index = i;
		c.path = dlStr(cards[i].path);
		c.title = dlStr(cards[i].title);
		c.badge = dlStr(cards[i].badge);
		c.sub = dlStr(cards[i].sub);
		c.used = cards[i].used;
		c.low = cards[i].low;
		c.icon = [[NSWorkspace sharedWorkspace] iconForFile:c.path];
		[_cards addObject:c];
		[self addSubview:c];
	}
	_isLoading = loading;
	_loading.stringValue = loading ? @"正在读取磁盘…" : @"没有找到磁盘";
	[dlRoot resizeSubviewsWithOldSize:dlRoot.bounds.size];
}

- (void)setTip:(bool)show {
	_showTip = show;
	[dlRoot resizeSubviewsWithOldSize:dlRoot.bounds.size];
}

// layoutForWidth 按宽度摆好所有东西，返回内容的高度
- (CGFloat)layoutForWidth:(CGFloat)W {
	CGFloat gap = 14, cw = MIN(1000, W - 64), x0 = floor((W - cw) / 2), y = 34;
	_title.frame = NSMakeRect(x0, y, cw, 34);
	y += 40;
	_sub.frame = NSMakeRect(x0, y, cw, 20);
	y += 20 + 26;

	int n = (int)_cards.count;
	int cols = MAX(1, (int)((cw + gap) / (290 + gap)));
	CGFloat cardW = floor((cw - (cols - 1) * gap) / cols), cardH = 96;
	for (int i = 0; i < n; i++) {
		_cards[i].frame = NSMakeRect(x0 + (i % cols) * (cardW + gap), y + (i / cols) * (cardH + gap), cardW, cardH);
	}
	_loading.hidden = n > 0;
	_loading.frame = NSMakeRect(x0, y + 10, cw, 20);
	int rows = n == 0 ? 1 : (n + cols - 1) / cols;
	y += rows * (cardH + gap) + 10;

	CGFloat bh = 32;
	[_pick sizeToFit];
	[_scan sizeToFit];
	CGFloat pw = MAX(_pick.frame.size.width + 8, 130), sw = MAX(_scan.frame.size.width + 8, 72);
	_pick.frame = NSMakeRect(x0, y, pw, bh);
	_scan.frame = NSMakeRect(x0 + cw - sw, y, sw, bh);
	CGFloat fh = MAX(_pathField.intrinsicContentSize.height, 28);
	_pathField.frame = NSMakeRect(x0 + pw + 10, y + floor((bh - fh) / 2), cw - pw - sw - 20, fh);
	y += bh + 26;

	_tip.hidden = !_showTip;
	if (_showTip) {
		[_tipButton sizeToFit];
		CGFloat bw = _tipButton.frame.size.width + 8;
		CGFloat textW = cw - 18 - 16 - bw - 16;
		CGFloat th = dlTextHeight(_tipText.stringValue, _tipText.font, textW);
		CGFloat boxH = MAX(66, 12 + 20 + 2 + th + 12);
		_tip.frame = NSMakeRect(x0, y, cw, boxH);
		_tipTitle.frame = NSMakeRect(18, 12, textW, 20);
		_tipText.frame = NSMakeRect(18, 34, textW, th);
		_tipButton.frame = NSMakeRect(cw - 16 - bw, floor((boxH - bh) / 2), bw, bh);
		y += boxH + 18;
	}
	_hint.frame = NSMakeRect(x0, y, cw, 20);
	y += 20 + 30;
	return y;
}
@end

// ---------------------------------------------------------------- 右侧列表

@interface DLRowData : NSObject
@property(nonatomic, copy) NSString *name, *size, *files, *path;
@property(nonatomic) double share;
@property(nonatomic) int flags;
@end
@implementation DLRowData
@end

// 「占比」一栏：一条小进度条加百分数
@interface DLShareView : NSView
@property(nonatomic) double share;
@end
@implementation DLShareView
- (BOOL)isFlipped { return YES; }
- (void)drawRect:(NSRect)dirty {
	NSRect b = self.bounds;
	CGFloat h = 5, bw = 30, y = floor((b.size.height - h) / 2);
	NSRect bar = NSMakeRect(4, y, bw, h);
	dlFillRound(bar, h / 2, dlColor(0xE6EAF0));
	CGFloat w = floor(bw * MIN(MAX(self.share, 0), 1));
	if (w > 0) dlFillRound(NSMakeRect(4, y, MAX(w, h), h), h / 2, dlColor(0x5B9BF8));
	double p = self.share * 100;
	NSString *s = p <= 0 ? @"0%" : p < 0.1 ? @"<0.1%" : p < 10 ? [NSString stringWithFormat:@"%.1f%%", p] : [NSString stringWithFormat:@"%.0f%%", p];
	dlDrawLine(s, [NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightRegular], dlColor(C_TEXT2),
	           NSMakeRect(bw + 10, 0, b.size.width - bw - 10, b.size.height), NSTextAlignmentLeft, NSLineBreakByClipping);
}
@end

@interface DLTableView : NSTableView
@end
@implementation DLTableView
- (void)keyDown:(NSEvent *)e {
	if (!dlHandleKey(e)) [super keyDown:e];
}
- (NSMenu *)menuForEvent:(NSEvent *)e {
	NSInteger row = [self rowAtPoint:[self convertPoint:e.locationInWindow fromView:nil]];
	if (row >= 0) {
		[self selectRowIndexes:[NSIndexSet indexSetWithIndex:row] byExtendingSelection:NO];
		goListMenu((int)row);
	}
	return nil;
}
- (void)copy:(id)sender { goCommand(DL_CMD_COPY_PATH); }
@end

// DLLinkButton 是一个看起来像链接的按钮
@interface DLLinkButton : NSButton
@end
@implementation DLLinkButton
- (void)resetCursorRects { [self addCursorRect:self.bounds cursor:[NSCursor pointingHandCursor]]; }
@end

@implementation DLSidePanel {
	NSScrollView *_scroll;
	NSMutableDictionary<NSNumber *, DLRowData *> *_cache;
	NSMutableDictionary<NSString *, NSImage *> *_icons;
	int _count;
	bool _quiet;
	NSString *_name, *_size, *_meta, *_note, *_iconPath;
	NSImage *_folderIcon;
	DLLinkButton *_link;
	CGFloat _headH, _noteH;
}

- (instancetype)initWithFrame:(NSRect)f {
	self = [super initWithFrame:f];
	_cache = [NSMutableDictionary dictionary];
	_icons = [NSMutableDictionary dictionary];

	NSTableView *t = [[DLTableView alloc] initWithFrame:NSZeroRect];
	NSArray *idents = @[ @"name", @"size", @"share", @"files" ], *titles = @[ @"名称", @"大小", @"占比", @"文件数" ];
	CGFloat widths[] = {180, 72, 92, 60};
	for (int i = 0; i < 4; i++) {
		NSTableColumn *c = [[NSTableColumn alloc] initWithIdentifier:idents[i]];
		c.title = titles[i];
		c.width = widths[i];
		c.minWidth = i == 0 ? 80 : widths[i];
		c.resizingMask = i == 0 ? NSTableColumnAutoresizingMask : NSTableColumnNoResizing;
		if (i == 1 || i == 3) c.headerCell.alignment = NSTextAlignmentRight;
		[t addTableColumn:c];
	}
	t.columnAutoresizingStyle = NSTableViewFirstColumnOnlyAutoresizingStyle;
	t.usesAlternatingRowBackgroundColors = YES;
	t.rowHeight = 22;
	t.allowsEmptySelection = YES;
	t.allowsMultipleSelection = NO;
	t.allowsColumnReordering = NO;
	t.style = NSTableViewStyleFullWidth;
	t.dataSource = self;
	t.delegate = self;
	t.target = self;
	t.doubleAction = @selector(doubleClick:);
	self.table = t;

	_scroll = [[NSScrollView alloc] initWithFrame:NSZeroRect];
	_scroll.documentView = t;
	_scroll.hasVerticalScroller = YES;
	_scroll.autohidesScrollers = YES;
	_scroll.borderType = NSNoBorder;
	[self addSubview:_scroll];

	_link = [[DLLinkButton alloc] initWithFrame:NSZeroRect];
	_link.bordered = NO;
	_link.target = self;
	_link.action = @selector(linkClicked:);
	_link.hidden = YES;
	[self addSubview:_link];
	_folderIcon = [[NSWorkspace sharedWorkspace] iconForContentType:UTTypeFolder];
	return self;
}

- (BOOL)isFlipped { return YES; }

- (void)linkClicked:(id)sender { goCommand(DL_CMD_FDA); }

- (void)setName:(NSString *)name size:(NSString *)size meta:(NSString *)meta note:(NSString *)note link:(NSString *)link icon:(NSString *)iconPath {
	_name = name;
	_size = size;
	_meta = meta;
	_note = note.length ? note : nil;
	if (![_iconPath isEqualToString:iconPath]) {
		_iconPath = iconPath;
		_folderIcon = iconPath.length ? [[NSWorkspace sharedWorkspace] iconForFile:iconPath] : [[NSWorkspace sharedWorkspace] iconForContentType:UTTypeFolder];
	}
	_link.hidden = link.length == 0;
	if (link.length) {
		_link.attributedTitle = [[NSAttributedString alloc] initWithString:link
		                                                        attributes:@{NSFontAttributeName : font(12, false), NSForegroundColorAttributeName : dlColor(C_ACCENT)}];
	}
	[self layoutParts];
	[self setNeedsDisplay:YES];
}

- (void)layout {
	[super layout];
	[self layoutParts];
}

- (void)setFrameSize:(NSSize)s {
	[super setFrameSize:s];
	[self layoutParts];
}

- (void)layoutParts {
	CGFloat W = self.bounds.size.width, pad = 16;
	CGFloat y = 16 + 22 + 36 + 20 + 12;
	_noteH = 0;
	if (_note) {
		CGFloat tw = W - 2 * pad - 24;
		CGFloat th = dlTextHeight(_note, font(12, false), tw);
		_noteH = th + 24 + (_link.hidden ? 0 : 22);
		if (!_link.hidden) {
			[_link sizeToFit];
			_link.frame = NSMakeRect(pad + 12, y + 12 + th + 2, MIN(_link.frame.size.width, tw), 20);
		}
		y += _noteH + 12;
	}
	_headH = y;
	_scroll.frame = NSMakeRect(1, y, MAX(W - 1, 0), MAX(self.bounds.size.height - y, 0));
}

- (void)drawRect:(NSRect)dirty {
	NSRect b = self.bounds;
	[dlColor(C_BAR) setFill];
	NSRectFill(NSIntersectionRect(dirty, self.bounds)); // macOS 14 起 dirty 可能超出自己的范围，填满它会盖住旁边的视图
	[dlColor(C_LINE) setFill];
	NSRectFill(NSMakeRect(0, 0, 1, b.size.height));
	NSRectFill(NSMakeRect(0, _headH - 1, b.size.width, 1));
	if (!_name) return;
	CGFloat pad = 16, x = pad, right = b.size.width - pad, y = 16;
	[_folderIcon drawInRect:NSMakeRect(x, y + 1, 20, 20) fromRect:NSZeroRect operation:NSCompositingOperationSourceOver fraction:1 respectFlipped:YES hints:nil];
	dlDrawLine(_name, font(13, true), dlColor(C_TEXT), NSMakeRect(x + 26, y, right - x - 26, 22), NSTextAlignmentLeft, NSLineBreakByTruncatingMiddle);
	y += 22;
	dlDrawLine(_size, font(24, true), dlColor(C_TEXT), NSMakeRect(x, y, right - x, 36), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
	y += 36;
	dlDrawLine(_meta, font(12, false), dlColor(C_TEXT2), NSMakeRect(x, y, right - x, 20), NSTextAlignmentLeft, NSLineBreakByTruncatingTail);
	y += 20 + 12;
	if (_note) {
		NSRect box = NSMakeRect(x, y, right - x, _noteH);
		dlFillRound(box, 10, dlColor(C_SOFT));
		CGFloat tw = box.size.width - 24;
		CGFloat th = dlTextHeight(_note, font(12, false), tw);
		dlDrawWrapped(_note, font(12, false), dlColor(C_TEXT2), NSMakeRect(x + 12, y + 12, tw, th));
	}
}

// ---- 列表数据：按需向 Go 要，一次刷新里缓存起来

- (void)reload:(int)n newFolder:(bool)newFolder {
	[_cache removeAllObjects];
	_count = n;
	_quiet = true;
	[self.table reloadData];
	if (newFolder && n > 0) [self.table scrollRowToVisible:0];
	_quiet = false;
}

- (void)selectRow:(int)row scroll:(bool)scroll {
	_quiet = true;
	if (row < 0 || row >= _count) {
		[self.table deselectAll:nil];
	} else {
		[self.table selectRowIndexes:[NSIndexSet indexSetWithIndex:row] byExtendingSelection:NO];
		if (scroll) [self.table scrollRowToVisible:row];
	}
	_quiet = false;
}

- (DLRowData *)rowData:(NSInteger)row {
	DLRowData *d = _cache[@(row)];
	if (d) return d;
	DLRow r = {0};
	goListRow((int)row, &r);
	d = [[DLRowData alloc] init];
	d.name = dlStr(r.name);
	d.size = dlStr(r.size);
	d.files = dlStr(r.files);
	d.path = dlStr(r.path);
	d.share = r.share;
	d.flags = r.flags;
	free(r.name);
	free(r.size);
	free(r.files);
	free(r.path);
	_cache[@(row)] = d;
	return d;
}

- (NSImage *)iconFor:(DLRowData *)d {
	NSString *key;
	NSString *ext = d.name.pathExtension.lowercaseString;
	if (d.flags & 1) {
		// 文件夹：应用（.app）、特殊文件夹有自己的图标，按路径取
		key = d.path;
	} else {
		key = [@"." stringByAppendingString:ext];
	}
	NSImage *img = _icons[key];
	if (img) return img;
	if (d.flags & 1) {
		img = [[NSWorkspace sharedWorkspace] iconForFile:d.path];
	} else {
		UTType *t = ext.length ? [UTType typeWithFilenameExtension:ext] : nil;
		img = [[NSWorkspace sharedWorkspace] iconForContentType:t ? t : UTTypeData];
	}
	if (img) _icons[key] = img;
	return img;
}

- (NSInteger)numberOfRowsInTableView:(NSTableView *)t { return _count; }

- (NSView *)tableView:(NSTableView *)t viewForTableColumn:(NSTableColumn *)col row:(NSInteger)row {
	DLRowData *d = [self rowData:row];
	NSString *ident = col.identifier;
	if ([ident isEqualToString:@"name"]) {
		NSTableCellView *cell = [t makeViewWithIdentifier:@"nameCell" owner:self];
		if (!cell) {
			cell = [[NSTableCellView alloc] initWithFrame:NSMakeRect(0, 0, 100, 22)];
			cell.identifier = @"nameCell";
			NSImageView *iv = [NSImageView imageViewWithImage:[[NSImage alloc] init]];
			iv.translatesAutoresizingMaskIntoConstraints = NO;
			NSTextField *tf = label(@"", font(13, false), [NSColor labelColor]);
			tf.translatesAutoresizingMaskIntoConstraints = NO;
			[cell addSubview:iv];
			[cell addSubview:tf];
			cell.imageView = iv;
			cell.textField = tf;
			[NSLayoutConstraint activateConstraints:@[
				[iv.leadingAnchor constraintEqualToAnchor:cell.leadingAnchor constant:2],
				[iv.centerYAnchor constraintEqualToAnchor:cell.centerYAnchor],
				[iv.widthAnchor constraintEqualToConstant:16],
				[iv.heightAnchor constraintEqualToConstant:16],
				[tf.leadingAnchor constraintEqualToAnchor:iv.trailingAnchor constant:6],
				[tf.trailingAnchor constraintEqualToAnchor:cell.trailingAnchor constant:-2],
				[tf.centerYAnchor constraintEqualToAnchor:cell.centerYAnchor],
			]];
		}
		cell.imageView.image = [self iconFor:d];
		NSString *name = d.name;
		if (d.flags & 2) name = [name stringByAppendingString:@"（无权限）"];
		cell.textField.stringValue = name;
		cell.textField.textColor = (d.flags & 2) ? [NSColor secondaryLabelColor] : [NSColor labelColor];
		return cell;
	}
	if ([ident isEqualToString:@"share"]) {
		DLShareView *v = [t makeViewWithIdentifier:@"shareCell" owner:self];
		if (!v) {
			v = [[DLShareView alloc] initWithFrame:NSMakeRect(0, 0, 92, 22)];
			v.identifier = @"shareCell";
		}
		v.share = d.share;
		[v setNeedsDisplay:YES];
		return v;
	}
	NSString *cid = [ident stringByAppendingString:@"Cell"];
	NSTextField *tf = [t makeViewWithIdentifier:cid owner:self];
	if (!tf) {
		tf = label(@"", [NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightRegular], [NSColor labelColor]);
		tf.identifier = cid;
		tf.alignment = NSTextAlignmentRight;
	}
	if ([ident isEqualToString:@"size"]) {
		tf.stringValue = d.size;
		tf.textColor = (d.flags & 4) ? dlColor(C_ACCENT) : [NSColor labelColor];
	} else {
		tf.stringValue = d.files;
		tf.textColor = [NSColor secondaryLabelColor];
	}
	return tf;
}

- (NSString *)tableView:(NSTableView *)t typeSelectStringForTableColumn:(NSTableColumn *)col row:(NSInteger)row {
	return [col.identifier isEqualToString:@"name"] ? [self rowData:row].name : nil;
}

- (void)tableViewSelectionDidChange:(NSNotification *)n {
	if (_quiet) return;
	goListSelect((int)self.table.selectedRow);
}

- (void)doubleClick:(id)sender {
	NSInteger row = self.table.clickedRow;
	if (row >= 0) goListOpen((int)row);
}
@end

// ---------------------------------------------------------------- 查看页

@implementation DLViewPage {
	NSArray<NSString *> *_legendNames;
	NSArray<NSColor *> *_legendColors;
	NSArray<NSString *> *_crumbPaths;
}

static const CGFloat kPathBarH = 32, kFootH = 26;

- (instancetype)initWithFrame:(NSRect)f {
	self = [super initWithFrame:f];
	self.sideOpen = true;
	self.pathControl = [[NSPathControl alloc] initWithFrame:NSZeroRect];
	self.pathControl.pathStyle = NSPathStyleStandard;
	self.pathControl.editable = NO;
	self.pathControl.backgroundColor = [NSColor clearColor];
	self.pathControl.font = font(13, false);
	self.pathControl.target = self;
	self.pathControl.action = @selector(crumbClicked:);
	self.map = [[DLMapView alloc] initWithFrame:NSZeroRect];
	self.side = [[DLSidePanel alloc] initWithFrame:NSZeroRect];
	// macOS 14 起视图默认不裁剪到自己的范围，树图和右侧面板挨着，各画各的
	if (@available(macOS 14.0, *)) {
		self.map.clipsToBounds = YES;
		self.side.clipsToBounds = YES;
	}
	[self addSubview:self.pathControl];
	[self addSubview:self.map];
	[self addSubview:self.side];
	return self;
}

- (BOOL)isFlipped { return YES; }

- (void)crumbClicked:(NSPathControl *)pc {
	NSPathControlItem *it = pc.clickedPathItem;
	if (!it) return;
	NSUInteger i = [pc.pathItems indexOfObject:it];
	if (i != NSNotFound) goCrumb((int)i);
}

- (void)setCrumbNames:(NSArray<NSString *> *)names paths:(NSArray<NSString *> *)paths {
	NSMutableArray *items = [NSMutableArray array];
	for (NSUInteger i = 0; i < names.count; i++) {
		NSPathControlItem *it = [[NSPathControlItem alloc] init];
		it.title = names[i];
		NSImage *img = i < paths.count ? [[NSWorkspace sharedWorkspace] iconForFile:paths[i]] : nil;
		img.size = NSMakeSize(16, 16);
		it.image = img;
		[items addObject:it];
	}
	self.pathControl.pathItems = items;
	_crumbPaths = paths;
}

- (void)setLegend:(NSArray<NSString *> *)names colors:(NSArray<NSColor *> *)colors {
	_legendNames = names;
	_legendColors = colors;
	[self setNeedsDisplay:YES];
}

- (void)setFooter:(NSString *)footer {
	_footer = [footer copy];
	NSRect b = self.bounds;
	[self setNeedsDisplayInRect:NSMakeRect(0, b.size.height - kFootH, b.size.width, kFootH)];
}

- (void)setSideOpen:(bool)open {
	_sideOpen = open;
	self.side.hidden = !open;
	[self resizeSubviewsWithOldSize:self.bounds.size];
}

- (void)resizeSubviewsWithOldSize:(NSSize)old {
	NSRect b = self.bounds;
	CGFloat W = b.size.width, H = b.size.height;
	CGFloat sideW = self.sideOpen ? floor(MIN(400, W * 0.4)) : 0;
	self.pathControl.frame = NSMakeRect(10, 4, W - 20, kPathBarH - 8);
	self.map.frame = NSMakeRect(0, kPathBarH, W - sideW, MAX(H - kPathBarH - kFootH, 0));
	self.side.frame = NSMakeRect(W - sideW, kPathBarH, sideW, MAX(H - kPathBarH - kFootH, 0));
}

- (void)drawRect:(NSRect)dirty {
	NSRect b = self.bounds;
	CGFloat W = b.size.width, H = b.size.height;
	[dlColor(C_BAR) setFill];
	NSRectFill(NSMakeRect(0, 0, W, kPathBarH));
	NSRectFill(NSMakeRect(0, H - kFootH, W, kFootH));
	[dlColor(C_LINE) setFill];
	NSRectFill(NSMakeRect(0, kPathBarH - 1, W, 1));
	NSRectFill(NSMakeRect(0, H - kFootH, W, 1));

	// 底栏右边：颜色图例；左边：鼠标指着的、选中的或者当前文件夹
	CGFloat pad = 14, right = W - pad;
	NSFont *fs = font(12, false);
	CGFloat total = 0;
	NSMutableArray *ws = [NSMutableArray array];
	for (NSString *n in _legendNames) {
		CGFloat w = 14 + dlTextWidth(n, fs) + 12;
		[ws addObject:@(w)];
		total += w;
	}
	if (_legendNames.count && total < W - 360) { // 左边至少留 360 点写路径
		CGFloat x = right - total, cy = H - kFootH / 2;
		for (NSUInteger i = 0; i < _legendNames.count; i++) {
			dlFillRound(NSMakeRect(x, cy - 5, 10, 10), 3, _legendColors[i]);
			dlDrawLine(_legendNames[i], fs, dlColor(C_TEXT2), NSMakeRect(x + 14, H - kFootH, [ws[i] doubleValue] - 14, kFootH), NSTextAlignmentLeft, NSLineBreakByClipping);
			x += [ws[i] doubleValue];
		}
		right -= total + 16;
	}
	dlDrawLine(self.footer, fs, dlColor(C_TEXT2), NSMakeRect(pad, H - kFootH, right - pad, kFootH), NSTextAlignmentLeft, NSLineBreakByTruncatingMiddle);
}
@end

// ---------------------------------------------------------------- 提示条和整个窗口

@interface DLToastView : NSView
@property(nonatomic, copy) NSString *text;
@property(nonatomic) bool error;
@end
@implementation DLToastView
- (BOOL)isFlipped { return YES; }
- (NSView *)hitTest:(NSPoint)p { return nil; } // 不挡鼠标
- (void)drawRect:(NSRect)dirty {
	NSRect b = NSInsetRect(self.bounds, 6, 6);
	[NSGraphicsContext saveGraphicsState];
	NSShadow *sh = [[NSShadow alloc] init];
	sh.shadowColor = [NSColor colorWithWhite:0 alpha:0.2];
	sh.shadowBlurRadius = 8;
	sh.shadowOffset = NSMakeSize(0, -2);
	[sh set];
	dlFillRound(b, 12, self.error ? dlColor(C_DANGER) : dlColorA(0x1F242E, 0.96));
	[NSGraphicsContext restoreGraphicsState];
	dlDrawLine(self.text, font(13, false), [NSColor whiteColor], NSInsetRect(b, 18, 0), NSTextAlignmentCenter, NSLineBreakByTruncatingMiddle);
}
@end

@implementation DLRootView {
	DLToastView *_toast;
	NSTimer *_toastTimer;
	bool _dragging;
}

- (instancetype)initWithFrame:(NSRect)f {
	self = [super initWithFrame:f];
	self.home = [[DLHomeView alloc] initWithFrame:NSMakeRect(0, 0, f.size.width, f.size.height)];
	self.homeScroll = [[NSScrollView alloc] initWithFrame:f];
	self.homeScroll.documentView = self.home;
	self.homeScroll.hasVerticalScroller = YES;
	self.homeScroll.autohidesScrollers = YES;
	self.homeScroll.drawsBackground = YES;
	self.homeScroll.backgroundColor = dlColor(C_BG);
	self.page = [[DLViewPage alloc] initWithFrame:f];
	self.page.hidden = YES;
	_toast = [[DLToastView alloc] initWithFrame:NSZeroRect];
	_toast.hidden = YES;
	[self addSubview:self.homeScroll];
	[self addSubview:self.page];
	[self addSubview:_toast];
	[self registerForDraggedTypes:@[ NSPasteboardTypeFileURL ]];
	return self;
}

- (BOOL)isFlipped { return YES; }

- (void)drawRect:(NSRect)dirty {
	[dlColor(C_BG) setFill];
	NSRectFill(NSIntersectionRect(dirty, self.bounds)); // macOS 14 起 dirty 可能超出自己的范围，填满它会盖住旁边的视图
}

- (void)setMode:(int)mode {
	_mode = mode;
	self.homeScroll.hidden = mode != 0;
	self.page.hidden = mode == 0;
	if (mode == 0) {
		[self.window makeFirstResponder:nil];
	} else {
		[self.window makeFirstResponder:self.page.map];
	}
	[self resizeSubviewsWithOldSize:self.bounds.size];
}

- (void)layout {
	[super layout];
	[self resizeSubviewsWithOldSize:self.bounds.size];
}

- (void)resizeSubviewsWithOldSize:(NSSize)old {
	NSRect b = self.bounds;
	self.homeScroll.frame = b;
	self.page.frame = b;
	CGFloat w = self.homeScroll.contentSize.width;
	CGFloat h = [self.home layoutForWidth:w];
	self.home.frame = NSMakeRect(0, 0, w, MAX(h, self.homeScroll.contentSize.height));
	[self placeToast];
}

- (void)placeToast {
	if (_toast.hidden) return;
	NSRect b = self.bounds;
	CGFloat w = MIN(dlTextWidth(_toast.text, font(13, false)) + 56 + 12, b.size.width - 40), h = 40 + 12;
	CGFloat bottom = self.mode == 1 ? 26 : 0;
	_toast.frame = NSMakeRect(floor((b.size.width - w) / 2), b.size.height - bottom - h - 14, w, h);
	[_toast setNeedsDisplay:YES];
}

- (void)toast:(NSString *)msg error:(bool)err {
	_toast.text = msg;
	_toast.error = err;
	_toast.hidden = NO;
	[self placeToast];
	[_toastTimer invalidate];
	_toastTimer = [NSTimer scheduledTimerWithTimeInterval:3.5 target:self selector:@selector(hideToast:) userInfo:nil repeats:NO];
}

- (void)hideToast:(NSTimer *)t {
	_toast.hidden = YES;
	_toastTimer = nil;
}

// ---- 把文件夹拖进窗口

- (NSString *)draggedPath:(id<NSDraggingInfo>)info {
	NSArray<NSURL *> *urls = [info.draggingPasteboard readObjectsForClasses:@[ NSURL.class ] options:@{NSPasteboardURLReadingFileURLsOnlyKey : @YES}];
	return urls.count ? urls[0].path : nil;
}

- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)info {
	return [self draggedPath:info] ? NSDragOperationGeneric : NSDragOperationNone;
}

- (BOOL)performDragOperation:(id<NSDraggingInfo>)info {
	NSString *p = [self draggedPath:info];
	if (!p) return NO;
	goOpenPath((char *)p.fileSystemRepresentation);
	return YES;
}
@end
