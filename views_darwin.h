// Mac 版界面内部用的类和小工具（只给 ui_darwin.m、views_darwin.m 用）

#import <Cocoa/Cocoa.h>
#include "cocoa_darwin.h"

// 配色，和 Windows 版一样：洁白、浅灰，强调色是蓝色
#define C_BG 0xF3F5F8
#define C_BAR 0xFBFCFD
#define C_LINE 0xE2E6ED
#define C_TEXT 0x15171C
#define C_TEXT2 0x5C616D
#define C_TEXT3 0x979CA8
#define C_ACCENT 0x0A6CFF
#define C_ACCENT_SOFT 0xE6F0FF
#define C_DANGER 0xE5484D
#define C_SOFT 0xEEF1F5

static inline NSColor *dlColor(uint32_t c) {
	return [NSColor colorWithSRGBRed:((c >> 16) & 0xFF) / 255.0 green:((c >> 8) & 0xFF) / 255.0 blue:(c & 0xFF) / 255.0 alpha:1];
}

static inline NSColor *dlColorA(uint32_t c, CGFloat a) {
	return [NSColor colorWithSRGBRed:((c >> 16) & 0xFF) / 255.0 green:((c >> 8) & 0xFF) / 255.0 blue:(c & 0xFF) / 255.0 alpha:a];
}

static inline NSString *dlStr(const char *s) {
	if (s == NULL) return @"";
	NSString *r = [NSString stringWithUTF8String:s];
	return r ? r : @"";
}

// 画一行字：在 r 里垂直居中，放不下时按 mode 省略
void dlDrawLine(NSString *s, NSFont *f, NSColor *c, NSRect r, NSTextAlignment align, NSLineBreakMode mode);
// 画多行字（自动换行，最多画到 r 的高度，最后一行放不下时加省略号）
void dlDrawWrapped(NSString *s, NSFont *f, NSColor *c, NSRect r);
CGFloat dlTextWidth(NSString *s, NSFont *f);
CGFloat dlTextHeight(NSString *s, NSFont *f, CGFloat width);
void dlFillRound(NSRect r, CGFloat radius, NSColor *c);
void dlRoundBox(NSRect r, CGFloat radius, NSColor *bg, NSColor *border);

// 树图
@interface DLMapView : NSView
- (void)setTiles:(DLTile *)tiles count:(int)n text:(char *)text scanning:(bool)scanning scale:(double)scale;
- (void)setMarksSel:(bool)sel rect:(NSRect)sr hover:(bool)hover rect:(NSRect)hr;
- (void)setTipName:(NSString *)name size:(NSString *)size share:(NSString *)share lines:(NSString *)lines path:(NSString *)path hint:(NSString *)hint;
- (void)setMessage:(NSString *)msg;
- (void)zoomFrom:(NSRect)from to:(NSRect)to useOld:(bool)useOld;
- (void)pixelWidth:(int32_t *)w height:(int32_t *)h scale:(double *)scale;
- (void)setMouseForShot:(NSPoint)p;
- (BOOL)animating;
@property(nonatomic) double forcedScale; // 截图模式：按这个倍数画（在 1 倍屏上也画出 Retina 的效果）
@end

// 树图和列表共用的按键处理，处理了返回 YES
BOOL dlHandleKey(NSEvent *e);

// 起始页
@interface DLHomeView : NSView
@property(nonatomic, strong) NSTextField *pathField;
- (void)setCards:(DLCard *)cards count:(int)n loading:(bool)loading;
- (void)setTip:(bool)show;
- (CGFloat)layoutForWidth:(CGFloat)w;
@end

// 右侧：当前文件夹的摘要 + 文件列表
@interface DLSidePanel : NSView <NSTableViewDataSource, NSTableViewDelegate>
@property(nonatomic, strong) NSTableView *table;
- (void)reload:(int)n newFolder:(bool)newFolder;
- (void)selectRow:(int)row scroll:(bool)scroll;
- (void)setName:(NSString *)name size:(NSString *)size meta:(NSString *)meta note:(NSString *)note link:(NSString *)link icon:(NSString *)iconPath;
@end

// 查看模式的整个页面：路径栏、树图、右侧、底栏
@interface DLViewPage : NSView
@property(nonatomic, strong) NSPathControl *pathControl;
@property(nonatomic, strong) DLMapView *map;
@property(nonatomic, strong) DLSidePanel *side;
@property(nonatomic) bool sideOpen;
@property(nonatomic, copy) NSString *footer;
- (void)setCrumbNames:(NSArray<NSString *> *)names paths:(NSArray<NSString *> *)paths;
- (void)setLegend:(NSArray<NSString *> *)names colors:(NSArray<NSColor *> *)colors;
@end

// 窗口内容：起始页和查看页二选一，上面浮着提示条
@interface DLRootView : NSView
@property(nonatomic, strong) NSScrollView *homeScroll;
@property(nonatomic, strong) DLHomeView *home;
@property(nonatomic, strong) DLViewPage *page;
@property(nonatomic) int mode;
- (void)toast:(NSString *)msg error:(bool)err;
@end

extern DLRootView *dlRoot;
extern NSWindow *dlWindow;
