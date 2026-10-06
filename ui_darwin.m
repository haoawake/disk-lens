// Mac 版的应用、窗口、菜单栏、工具栏、对话框，以及给 Go 调用的 dl* 函数。

#import "views_darwin.h"
#import <QuartzCore/QuartzCore.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#include <dlfcn.h>
#include "_cgo_export.h"

static bool shotMode;
static NSString *pendingOpen; // 启动完成前从访达「打开方式」传进来的路径
static bool launched;

// ---------------------------------------------------------------- 应用和窗口

@interface DLApp : NSObject <NSApplicationDelegate, NSWindowDelegate, NSToolbarDelegate>
@property(nonatomic, strong) NSButton *homeButton, *rescanButton;
@property(nonatomic, strong) NSSegmentedControl *detail;
@property(nonatomic, strong) NSProgressIndicator *spinner;
@property(nonatomic, strong) NSTimer *tick;
@property(nonatomic) bool busy;
@end

static DLApp *app;
static void applyToolbar(void);

static NSMenuItem *addItem(NSMenu *m, NSString *title, SEL action, NSString *key, NSEventModifierFlags mods, int tag) {
	NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:key ? key : @""];
	it.keyEquivalentModifierMask = mods;
	it.tag = tag;
	if (tag) it.target = app;
	[m addItem:it];
	return it;
}

static NSMenu *submenu(NSMenu *main, NSString *title) {
	NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:@""];
	NSMenu *m = [[NSMenu alloc] initWithTitle:title];
	it.submenu = m;
	[main addItem:it];
	return m;
}

static NSString *keyChar(unichar c) { return [NSString stringWithCharacters:&c length:1]; }

static void buildMenu(void) {
	const NSEventModifierFlags cmd = NSEventModifierFlagCommand, opt = NSEventModifierFlagOption, shift = NSEventModifierFlagShift,
	                           ctrl = NSEventModifierFlagControl;
	NSMenu *main = [[NSMenu alloc] initWithTitle:@""];

	NSMenu *m = submenu(main, @"文件清理助手");
	addItem(m, @"关于文件清理助手", @selector(menuCommand:), nil, 0, DL_CMD_ABOUT);
	[m addItem:[NSMenuItem separatorItem]];
	NSMenu *services = submenu(m, @"服务");
	NSApp.servicesMenu = services;
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"隐藏文件清理助手", @selector(hide:), @"h", cmd, 0);
	addItem(m, @"隐藏其他", @selector(hideOtherApplications:), @"h", cmd | opt, 0);
	addItem(m, @"全部显示", @selector(unhideAllApplications:), nil, 0, 0);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"退出文件清理助手", @selector(terminate:), @"q", cmd, 0);

	m = submenu(main, @"文件");
	addItem(m, @"选择文件夹…", @selector(menuCommand:), @"o", cmd, DL_CMD_PICK);
	addItem(m, @"换个磁盘", @selector(menuCommand:), @"d", cmd | shift, DL_CMD_HOME);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"打开", @selector(menuCommand:), keyChar(NSDownArrowFunctionKey), cmd, DL_CMD_OPEN);
	addItem(m, @"在访达中显示", @selector(menuCommand:), @"r", cmd | shift, DL_CMD_REVEAL);
	addItem(m, @"拷贝路径", @selector(menuCommand:), @"c", cmd | opt, DL_CMD_COPY_PATH);
	addItem(m, @"重新扫描这个文件夹", @selector(menuCommand:), nil, 0, DL_CMD_RESCAN_ITEM);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"移到废纸篓", @selector(menuCommand:), @"\b", cmd, DL_CMD_TRASH);
	addItem(m, @"永久删除…", @selector(menuCommand:), @"\b", cmd | opt, DL_CMD_DELETE);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"关闭窗口", @selector(performClose:), @"w", cmd, 0);

	m = submenu(main, @"编辑");
	addItem(m, @"撤销", @selector(undo:), @"z", cmd, 0);
	addItem(m, @"重做", @selector(redo:), @"z", cmd | shift, 0);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"剪切", @selector(cut:), @"x", cmd, 0);
	addItem(m, @"拷贝", @selector(copy:), @"c", cmd, 0);
	addItem(m, @"粘贴", @selector(paste:), @"v", cmd, 0);
	addItem(m, @"全选", @selector(selectAll:), @"a", cmd, 0);

	m = submenu(main, @"显示");
	addItem(m, @"粗略", @selector(menuCommand:), @"1", cmd, DL_CMD_DETAIL0);
	addItem(m, @"适中", @selector(menuCommand:), @"2", cmd, DL_CMD_DETAIL0 + 1);
	addItem(m, @"精细", @selector(menuCommand:), @"3", cmd, DL_CMD_DETAIL0 + 2);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"显示右侧列表", @selector(menuCommand:), @"s", cmd | ctrl, DL_CMD_SIDE);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"重新扫描", @selector(menuCommand:), @"r", cmd, DL_CMD_RESCAN_ALL);
	addItem(m, @"停止扫描", @selector(menuCommand:), @".", cmd, DL_CMD_STOP);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"进入全屏幕", @selector(toggleFullScreen:), @"f", cmd | ctrl, 0);

	m = submenu(main, @"前往");
	addItem(m, @"后退", @selector(menuCommand:), @"[", cmd, DL_CMD_BACK);
	addItem(m, @"前进", @selector(menuCommand:), @"]", cmd, DL_CMD_FORWARD);
	addItem(m, @"上一级", @selector(menuCommand:), keyChar(NSUpArrowFunctionKey), cmd, DL_CMD_UP);

	m = submenu(main, @"窗口");
	addItem(m, @"最小化", @selector(performMiniaturize:), @"m", cmd, 0);
	addItem(m, @"缩放", @selector(performZoom:), nil, 0, 0);
	[m addItem:[NSMenuItem separatorItem]];
	addItem(m, @"前置全部窗口", @selector(arrangeInFront:), nil, 0, 0);
	NSApp.windowsMenu = m;

	m = submenu(main, @"帮助");
	addItem(m, @"文件清理助手使用说明", @selector(menuCommand:), nil, 0, DL_CMD_HELP);
	addItem(m, @"「完全磁盘访问权限」是什么…", @selector(menuCommand:), nil, 0, DL_CMD_FDA);
	NSApp.helpMenu = m;

	NSApp.mainMenu = main;
}

static NSToolbarItemIdentifier const kBack = @"back", kForward = @"forward", kUp = @"up", kHome = @"home", kSpinner = @"spinner",
                                     kRescan = @"rescan", kDetail = @"detail", kSide = @"side";

@implementation DLApp

- (void)menuCommand:(NSMenuItem *)it {
	if (it.tag == DL_CMD_ABOUT) {
		NSString *credits = @"像 SpaceSniffer 一样，看看磁盘空间都去哪了。\nhttps://github.com/haoawake/disk-lens";
		NSMutableParagraphStyle *ps = [[NSMutableParagraphStyle alloc] init];
		ps.alignment = NSTextAlignmentCenter;
		[NSApp orderFrontStandardAboutPanelWithOptions:@{
			NSAboutPanelOptionCredits : [[NSAttributedString alloc] initWithString:credits
			                                                            attributes:@{NSFontAttributeName : [NSFont systemFontOfSize:11], NSParagraphStyleAttributeName : ps}]
		}];
		return;
	}
	goCommand((int)it.tag);
}

- (void)toolbarCommand:(id)sender { goCommand((int)[sender tag]); }

- (BOOL)validateMenuItem:(NSMenuItem *)it {
	if (it.tag == 0 || it.tag == DL_CMD_ABOUT) return YES;
	int v = goValidate((int)it.tag);
	it.state = (v & 2) ? NSControlStateValueOn : NSControlStateValueOff;
	return (v & 1) != 0;
}

- (BOOL)validateToolbarItem:(NSToolbarItem *)it { return (goValidate((int)it.tag) & 1) != 0; }

- (void)detailChanged:(NSSegmentedControl *)s { goCommand(DL_CMD_DETAIL0 + (int)s.selectedSegment); }

- (void)tickTimer:(NSTimer *)t { goTick(); }

// ---- 启动、退出

- (void)applicationWillFinishLaunching:(NSNotification *)n {
	// 树图的配色是给浅色界面设计的，统一用浅色外观，深色模式下也不会一半深一半浅
	NSApp.appearance = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
	buildMenu();
}

- (void)application:(NSApplication *)a openURLs:(NSArray<NSURL *> *)urls {
	NSString *p = urls.firstObject.path;
	if (!p) return;
	if (!launched) {
		pendingOpen = p;
	} else {
		goOpenPath((char *)p.fileSystemRepresentation);
	}
}

- (void)applicationDidFinishLaunching:(NSNotification *)n {
	NSRect frame = NSMakeRect(0, 0, shotMode ? 1280 : 1200, shotMode ? 800 : 780);
	NSWindow *w = [[NSWindow alloc] initWithContentRect:frame
	                                          styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
	                                            backing:NSBackingStoreBuffered
	                                              defer:NO];
	w.title = @"文件清理助手";
	w.contentMinSize = NSMakeSize(860, 540);
	w.delegate = self;
	w.releasedWhenClosed = NO;
	w.tabbingMode = NSWindowTabbingModeDisallowed;
	dlWindow = w;

	NSToolbar *tb = [[NSToolbar alloc] initWithIdentifier:@"main"];
	tb.delegate = self;
	tb.displayMode = NSToolbarDisplayModeIconOnly;
	tb.allowsUserCustomization = NO;
	w.toolbar = tb;
	w.toolbarStyle = NSWindowToolbarStyleUnified;

	dlRoot = [[DLRootView alloc] initWithFrame:frame];
	w.contentView = dlRoot;
	if (shotMode) {
		dlRoot.page.map.forcedScale = 2;
		[w setContentSize:frame.size];
		[w center];
	} else {
		[w center];
		[w setFrameAutosaveName:@"DiskLensMainWindow"]; // 记住窗口大小和位置
	}
	[w makeKeyAndOrderFront:nil];
	[NSApp activateIgnoringOtherApps:YES];

	launched = true;
	NSString *p = pendingOpen;
	pendingOpen = nil;
	goLaunched(p ? (char *)p.fileSystemRepresentation : NULL);
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)a { return YES; }

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)a {
	return goShouldClose() ? NSTerminateNow : NSTerminateCancel;
}

- (BOOL)windowShouldClose:(NSWindow *)w { return goShouldClose() != 0; }

// ---- 工具栏

- (NSArray<NSToolbarItemIdentifier> *)toolbarDefaultItemIdentifiers:(NSToolbar *)tb {
	return @[ kBack, kForward, kUp, kHome, NSToolbarFlexibleSpaceItemIdentifier, kSpinner, kRescan, kDetail, kSide ];
}

- (NSArray<NSToolbarItemIdentifier> *)toolbarAllowedItemIdentifiers:(NSToolbar *)tb {
	return [self toolbarDefaultItemIdentifiers:tb];
}

- (NSToolbarItem *)imageItem:(NSToolbarItemIdentifier)ident symbol:(NSString *)sym label:(NSString *)label tip:(NSString *)tip cmd:(int)cmd {
	NSToolbarItem *it = [[NSToolbarItem alloc] initWithItemIdentifier:ident];
	it.image = [NSImage imageWithSystemSymbolName:sym accessibilityDescription:label];
	it.label = label;
	it.toolTip = tip;
	it.tag = cmd;
	it.target = self;
	it.action = @selector(toolbarCommand:);
	it.bordered = YES;
	return it;
}

static NSButton *toolbarButton(NSString *title, NSString *sym, int cmd) {
	NSButton *b = [NSButton buttonWithTitle:title image:[NSImage imageWithSystemSymbolName:sym accessibilityDescription:nil] target:app action:@selector(toolbarCommand:)];
	b.imagePosition = NSImageLeading;
	b.bezelStyle = NSBezelStyleTexturedRounded;
	b.tag = cmd;
	return b;
}

- (NSToolbarItem *)toolbar:(NSToolbar *)tb itemForItemIdentifier:(NSToolbarItemIdentifier)ident willBeInsertedIntoToolbar:(BOOL)flag {
	NSToolbarItem *it = nil;
	if ([ident isEqualToString:kBack]) {
		it = [self imageItem:ident symbol:@"chevron.left" label:@"后退" tip:@"后退（⌘[）" cmd:DL_CMD_BACK];
	} else if ([ident isEqualToString:kForward]) {
		it = [self imageItem:ident symbol:@"chevron.right" label:@"前进" tip:@"前进（⌘]）" cmd:DL_CMD_FORWARD];
	} else if ([ident isEqualToString:kUp]) {
		it = [self imageItem:ident symbol:@"arrow.up" label:@"上一级" tip:@"上一级（⌘↑ 或 ⌫）" cmd:DL_CMD_UP];
	} else if ([ident isEqualToString:kSide]) {
		it = [self imageItem:ident symbol:@"sidebar.right" label:@"右侧列表" tip:@"显示或隐藏右侧列表（⌃⌘S）" cmd:DL_CMD_SIDE];
	} else if ([ident isEqualToString:kHome]) {
		it = [[NSToolbarItem alloc] initWithItemIdentifier:ident];
		self.homeButton = toolbarButton(@"换个磁盘", @"internaldrive", DL_CMD_HOME);
		it.view = self.homeButton;
		it.label = @"换个磁盘";
		it.toolTip = @"回到起始页，选别的磁盘或文件夹";
	} else if ([ident isEqualToString:kRescan]) {
		it = [[NSToolbarItem alloc] initWithItemIdentifier:ident];
		self.rescanButton = toolbarButton(@"重新扫描", @"arrow.clockwise", DL_CMD_STOP_OR_RESCAN);
		it.view = self.rescanButton;
		it.label = @"重新扫描";
		it.toolTip = @"重新扫描整个磁盘（⌘R）";
	} else if ([ident isEqualToString:kSpinner]) {
		it = [[NSToolbarItem alloc] initWithItemIdentifier:ident];
		self.spinner = [[NSProgressIndicator alloc] initWithFrame:NSMakeRect(0, 0, 16, 16)];
		self.spinner.style = NSProgressIndicatorStyleSpinning;
		self.spinner.controlSize = NSControlSizeSmall;
		self.spinner.displayedWhenStopped = NO;
		it.view = self.spinner;
		it.label = @"";
	} else if ([ident isEqualToString:kDetail]) {
		it = [[NSToolbarItem alloc] initWithItemIdentifier:ident];
		self.detail = [NSSegmentedControl segmentedControlWithLabels:@[ @"粗略", @"适中", @"精细" ]
		                                                trackingMode:NSSegmentSwitchTrackingSelectOne
		                                                      target:self
		                                                      action:@selector(detailChanged:)];
		self.detail.selectedSegment = 1;
		it.view = self.detail;
		it.label = @"细节";
		it.toolTip = @"方块画得多细：越精细，小文件也越容易看到（⌘1、⌘2、⌘3）";
	}
	if (it.view) dispatch_async(dispatch_get_main_queue(), ^{
	  applyToolbar();
	});
	// 后退、前进、上一级放在标题左边，和访达一样
	SEL nav = NSSelectorFromString(@"setNavigational:");
	if (it && ([ident isEqualToString:kBack] || [ident isEqualToString:kForward] || [ident isEqualToString:kUp]) && [it respondsToSelector:nav]) {
		((void (*)(id, SEL, BOOL))[it methodForSelector:nav])(it, nav, YES);
	}
	return it;
}
@end

// ---------------------------------------------------------------- 给 Go 调用

void dlRun(bool shot) {
	shotMode = shot;
	[NSApplication sharedApplication];
	app = [[DLApp alloc] init];
	NSApp.delegate = app;
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSApp run];
}

void dlTerminate(void) { [NSApp terminate:nil]; }

// 用 CFRunLoopPerformBlock 而不是 GCD 的主队列：截图模式、对话框里会再开一层事件循环，
// 主队列里的任务在那时不会被执行，run loop 上的任务会
void dlPost(int64_t tag) {
	CFRunLoopRef main = CFRunLoopGetMain();
	CFRunLoopPerformBlock(main, kCFRunLoopCommonModes, ^{
	  goPosted(tag);
	});
	CFRunLoopWakeUp(main);
}

void dlAfter(int ms, int64_t tag) {
	NSTimer *t = [NSTimer timerWithTimeInterval:ms / 1000.0
	                                    repeats:NO
	                                      block:^(NSTimer *timer) {
		                                    goPosted(tag);
	                                    }];
	[[NSRunLoop mainRunLoop] addTimer:t forMode:NSRunLoopCommonModes];
}

void dlStartTimer(void) {
	if (app.tick) return;
	app.tick = [NSTimer timerWithTimeInterval:0.1 target:app selector:@selector(tickTimer:) userInfo:nil repeats:YES];
	[[NSRunLoop currentRunLoop] addTimer:app.tick forMode:NSRunLoopCommonModes];
}

void dlStopTimer(void) {
	[app.tick invalidate];
	app.tick = nil;
}

void dlRunLoopFor(int ms) {
	NSDate *until = [NSDate dateWithTimeIntervalSinceNow:ms / 1000.0];
	while ([until timeIntervalSinceNow] > 0) {
		@autoreleasepool {
			NSEvent *e = [NSApp nextEventMatchingMask:NSEventMaskAny untilDate:[NSDate dateWithTimeIntervalSinceNow:0.01] inMode:NSDefaultRunLoopMode dequeue:YES];
			if (e) [NSApp sendEvent:e];
			[NSApp updateWindows];
		}
	}
}

void dlSetMode(int mode) { dlRoot.mode = mode; }

void dlSetTitle(const char *title, const char *subtitle) {
	dlWindow.title = dlStr(title);
	dlWindow.subtitle = dlStr(subtitle);
}

// 工具栏的状态先记下来：工具栏上的按钮是系统按需创建的，建好时再套用一次
static struct {
	bool back, fwd, up, busy, rescan, side, view, set;
	int detail;
} tbState;
static NSString *tbHome = @"";

static void applyToolbar(void) {
	if (!tbState.set) return;
	app.homeButton.enabled = tbHome.length > 0;
	if (tbHome.length) {
		app.homeButton.title = tbHome;
		app.homeButton.image = [NSImage imageWithSystemSymbolName:tbState.view ? @"internaldrive" : @"arrow.uturn.backward" accessibilityDescription:nil];
	}
	app.rescanButton.title = tbState.busy ? @"停止" : @"重新扫描";
	app.rescanButton.image = [NSImage imageWithSystemSymbolName:tbState.busy ? @"stop.circle" : @"arrow.clockwise" accessibilityDescription:nil];
	app.rescanButton.enabled = tbState.busy || tbState.rescan;
	if (tbState.busy != app.busy) {
		app.busy = tbState.busy;
		if (app.busy) {
			[app.spinner startAnimation:nil];
		} else {
			[app.spinner stopAnimation:nil];
		}
	}
	app.detail.enabled = tbState.view;
	app.detail.selectedSegment = tbState.detail;
}

void dlSetToolbar(const char *home, bool back, bool fwd, bool up, bool busy, bool rescan, int detail, bool side, bool view) {
	tbHome = dlStr(home);
	tbState.back = back;
	tbState.fwd = fwd;
	tbState.up = up;
	tbState.busy = busy;
	tbState.rescan = rescan;
	tbState.detail = detail;
	tbState.side = side;
	tbState.view = view;
	tbState.set = true;
	applyToolbar();
	if (dlRoot.page.sideOpen != side) dlRoot.page.sideOpen = side;
	[dlWindow.toolbar validateVisibleItems];
}

void dlSetCrumbs(const char *names, const char *paths) {
	[dlRoot.page setCrumbNames:[dlStr(names) componentsSeparatedByString:@"\n"] paths:[dlStr(paths) componentsSeparatedByString:@"\n"]];
}

void dlSetFooter(const char *text) { dlRoot.page.footer = dlStr(text); }

void dlSetLegend(const char *names, const uint32_t *colors, int n) {
	NSArray *ns = [dlStr(names) componentsSeparatedByString:@"\n"];
	NSMutableArray *cs = [NSMutableArray array];
	for (int i = 0; i < n; i++) [cs addObject:dlColor(colors[i])];
	[dlRoot.page setLegend:ns colors:cs];
}

void dlToast(const char *msg, bool isErr) { [dlRoot toast:dlStr(msg) error:isErr]; }

void dlSetCards(DLCard *cards, int n, bool loading) { [dlRoot.home setCards:cards count:n loading:loading]; }

void dlSetHomeTip(bool show) { [dlRoot.home setTip:show]; }

void dlMapSize(int32_t *w, int32_t *h, double *scale) { [dlRoot.page.map pixelWidth:w height:h scale:scale]; }

void dlMapSetTiles(DLTile *tiles, int n, char *text, bool scanning, double scale) {
	[dlRoot.page.map setTiles:tiles count:n text:text scanning:scanning scale:scale];
}

static NSRect pxRect(int32_t x0, int32_t y0, int32_t x1, int32_t y1) { return NSMakeRect(x0, y0, x1 - x0, y1 - y0); }

void dlMapSetMarks(bool sel, int32_t sx0, int32_t sy0, int32_t sx1, int32_t sy1, bool hover, int32_t hx0, int32_t hy0, int32_t hx1, int32_t hy1) {
	[dlRoot.page.map setMarksSel:sel rect:pxRect(sx0, sy0, sx1, sy1) hover:hover rect:pxRect(hx0, hy0, hx1, hy1)];
}

void dlMapSetTip(const char *name, const char *size, const char *share, const char *lines, const char *path, const char *hint) {
	if (!name) {
		[dlRoot.page.map setTipName:nil size:nil share:nil lines:nil path:nil hint:nil];
		return;
	}
	[dlRoot.page.map setTipName:dlStr(name) size:dlStr(size) share:dlStr(share) lines:dlStr(lines) path:dlStr(path) hint:dlStr(hint)];
}

void dlMapSetMessage(const char *msg) { [dlRoot.page.map setMessage:dlStr(msg)]; }

void dlMapShotMouse(double x, double y) { [dlRoot.page.map setMouseForShot:NSMakePoint(x, y)]; }

void dlMapZoom(int32_t fx0, int32_t fy0, int32_t fx1, int32_t fy1, int32_t tx0, int32_t ty0, int32_t tx1, int32_t ty1, bool useOld) {
	[dlRoot.page.map zoomFrom:pxRect(fx0, fy0, fx1, fy1) to:pxRect(tx0, ty0, tx1, ty1) useOld:useOld];
}

void dlListReload(int n, bool newFolder) { [dlRoot.page.side reload:n newFolder:newFolder]; }

void dlListSelect(int row, bool scroll) { [dlRoot.page.side selectRow:row scroll:scroll]; }

void dlSetSide(const char *name, const char *size, const char *meta, const char *note, const char *link, const char *iconPath, bool visible) {
	[dlRoot.page.side setName:dlStr(name) size:dlStr(size) meta:dlStr(meta) note:dlStr(note) link:dlStr(link) icon:dlStr(iconPath)];
}

// ---------------------------------------------------------------- 截图

static NSData *pngOfView(NSView *v, CGFloat scale) {
	NSRect b = v.bounds;
	NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL
	                                                                pixelsWide:(NSInteger)(b.size.width * scale)
	                                                                pixelsHigh:(NSInteger)(b.size.height * scale)
	                                                             bitsPerSample:8
	                                                           samplesPerPixel:4
	                                                                  hasAlpha:YES
	                                                                  isPlanar:NO
	                                                            colorSpaceName:NSCalibratedRGBColorSpace
	                                                               bytesPerRow:0
	                                                              bitsPerPixel:0];
	rep.size = b.size;
	[v cacheDisplayInRect:b toBitmapImageRep:rep];
	return [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
}

// 系统截屏（CGWindowListCreateImage 在新系统上已经不建议用，按名字找，找不到就算了）
typedef CGImageRef (*windowListImageFn)(CGRect, uint32_t, uint32_t, uint32_t);

static bool screenShot(NSWindow *w, NSString *file) {
	windowListImageFn fn = (windowListImageFn)dlsym(RTLD_DEFAULT, "CGWindowListCreateImage");
	if (!fn || !w) return false;
	// 屏幕坐标：CG 用左上角为原点
	NSRect f = w.frame;
	CGFloat screenH = NSScreen.screens.firstObject.frame.size.height;
	CGRect r = CGRectMake(f.origin.x, screenH - f.origin.y - f.size.height, f.size.width, f.size.height);
	CGImageRef img = fn(r, 1 /* kCGWindowListOptionOnScreenOnly */, 0, 1 /* kCGWindowImageBoundsIgnoreFraming */);
	if (!img) return false;
	NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:img];
	CGImageRelease(img);
	return [[rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:file atomically:YES];
}

bool dlCapture(const char *file) {
	NSString *f = dlStr(file);
	[dlWindow displayIfNeeded];
	// 整个窗口（包括标题栏和工具栏）按 2 倍画，在 1 倍屏的电脑上也能看清 Retina 的效果
	NSView *frameView = dlWindow.contentView.superview;
	bool ok = [pngOfView(frameView, 2) writeToFile:f atomically:YES];
	screenShot(dlWindow, [[f stringByDeletingPathExtension] stringByAppendingString:@"-screen.png"]);
	return ok;
}

void dlLog(const char *msg) {
	fprintf(stdout, "%s\n", msg);
	fflush(stdout);
}

// ---------------------------------------------------------------- 对话框

@interface DLAlertHelper : NSObject
@property(nonatomic, weak) NSButton *guarded;
@property(nonatomic, weak) NSButton *check;
@end
@implementation DLAlertHelper
- (void)checkChanged:(NSButton *)b { self.guarded.enabled = b.state == NSControlStateValueOn; }
@end

int dlAlert(int style, const char *title, const char *msg, const char *buttons, int def, int destructive, const char *check, int needCheck,
            const char *details, bool *checked, const char *shotFile, int shotClick, bool shotCheck) {
	NSAlert *a = [[NSAlert alloc] init];
	a.alertStyle = style == 2 ? NSAlertStyleCritical : style == 1 ? NSAlertStyleWarning : NSAlertStyleInformational;
	a.messageText = dlStr(title);
	a.informativeText = dlStr(msg);
	NSArray<NSString *> *titles = [dlStr(buttons) componentsSeparatedByString:@"\n"];
	for (NSString *t in titles) [a addButtonWithTitle:t];
	for (NSUInteger i = 0; i < a.buttons.count; i++) {
		NSButton *b = a.buttons[i];
		b.keyEquivalent = (int)i == def ? @"\r" : ([titles[i] isEqualToString:@"取消"] ? @"\033" : @"");
		if ((int)i == destructive) b.hasDestructiveAction = YES;
	}
	DLAlertHelper *helper = [[DLAlertHelper alloc] init];
	NSString *chk = dlStr(check);
	if (chk.length) {
		a.showsSuppressionButton = YES;
		a.suppressionButton.title = chk;
		a.suppressionButton.state = NSControlStateValueOff;
		helper.check = a.suppressionButton;
		if (needCheck >= 0 && needCheck < (int)a.buttons.count) {
			helper.guarded = a.buttons[needCheck];
			helper.guarded.enabled = NO;
			a.suppressionButton.target = helper;
			a.suppressionButton.action = @selector(checkChanged:);
		}
	}
	NSString *det = dlStr(details);
	if (det.length) {
		NSScrollView *sv = [[NSScrollView alloc] initWithFrame:NSMakeRect(0, 0, 440, 150)];
		sv.hasVerticalScroller = YES;
		sv.borderType = NSBezelBorder;
		NSTextView *tv = [[NSTextView alloc] initWithFrame:NSMakeRect(0, 0, 440, 150)];
		tv.editable = NO;
		tv.string = det;
		tv.font = [NSFont systemFontOfSize:11];
		tv.textContainerInset = NSMakeSize(4, 4);
		tv.verticallyResizable = YES;
		tv.autoresizingMask = NSViewWidthSizable;
		sv.documentView = tv;
		a.accessoryView = sv;
	}

	// 截图模式：过一会儿把对话框截下来，然后替用户点按钮
	NSString *sf = dlStr(shotFile);
	if (sf.length) {
		NSTimer *t = [NSTimer timerWithTimeInterval:0.8
		                                    repeats:NO
		                                      block:^(NSTimer *timer) {
			                                    [a.window displayIfNeeded];
			                                    [pngOfView(a.window.contentView.superview, 2) writeToFile:sf atomically:YES];
			                                    screenShot(dlWindow, [[sf stringByDeletingPathExtension] stringByAppendingString:@"-screen.png"]);
			                                    if (shotCheck && a.suppressionButton) {
				                                    a.suppressionButton.state = NSControlStateValueOn;
				                                    [helper checkChanged:a.suppressionButton];
			                                    }
			                                    int i = shotClick >= 0 && shotClick < (int)a.buttons.count ? shotClick : (int)a.buttons.count - 1;
			                                    [a.buttons[i] performClick:nil];
		                                    }];
		[[NSRunLoop currentRunLoop] addTimer:t forMode:NSRunLoopCommonModes];
		[[NSRunLoop currentRunLoop] addTimer:t forMode:NSModalPanelRunLoopMode];
	}

	NSModalResponse r;
	if (dlWindow.visible && !dlWindow.attachedSheet) {
		// 以表单的形式挂在窗口上（Mac 的习惯），但仍然等用户点完再返回
		[a beginSheetModalForWindow:dlWindow
		          completionHandler:^(NSModalResponse code) {
			          [NSApp stopModalWithCode:code];
		          }];
		r = [NSApp runModalForWindow:a.window];
	} else {
		r = [a runModal];
	}
	if (checked) *checked = a.suppressionButton.state == NSControlStateValueOn;
	return (int)(r - NSAlertFirstButtonReturn);
}

void dlPickFolder(void) {
	NSOpenPanel *p = [NSOpenPanel openPanel];
	p.canChooseDirectories = YES;
	p.canChooseFiles = NO;
	p.allowsMultipleSelection = NO;
	p.prompt = @"扫描";
	p.message = @"选择要分析的文件夹";
	[p beginSheetModalForWindow:dlWindow
	          completionHandler:^(NSModalResponse r) {
		          if (r == NSModalResponseOK && p.URL) goOpenPath((char *)p.URL.path.fileSystemRepresentation);
	          }];
}

@interface DLMenuTarget : NSObject
@end
@implementation DLMenuTarget
- (void)pick:(NSMenuItem *)it { goMenuPick((int)it.tag); }
@end

static DLMenuTarget *menuTarget;

void dlPopupMenu(const char *items) {
	if (!menuTarget) menuTarget = [[DLMenuTarget alloc] init];
	NSMenu *m = [[NSMenu alloc] initWithTitle:@""];
	m.autoenablesItems = NO;
	for (NSString *line in [dlStr(items) componentsSeparatedByString:@"\n"]) {
		NSArray<NSString *> *f = [line componentsSeparatedByString:@"\t"];
		if (f.count < 4) continue;
		int flags = f[1].intValue;
		if (flags & 2) {
			[m addItem:[NSMenuItem separatorItem]];
			continue;
		}
		NSString *key = f[2];
		if ([key isEqualToString:@"up"]) key = keyChar(NSUpArrowFunctionKey);
		if ([key isEqualToString:@"down"]) key = keyChar(NSDownArrowFunctionKey);
		if ([key isEqualToString:@"del"]) key = @"\b";
		NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:f[3] action:@selector(pick:) keyEquivalent:key];
		NSEventModifierFlags mods = 0;
		if (flags & 4) mods |= NSEventModifierFlagCommand;
		if (flags & 8) mods |= NSEventModifierFlagOption;
		if (flags & 16) mods |= NSEventModifierFlagShift;
		it.keyEquivalentModifierMask = mods;
		it.tag = f[0].intValue;
		it.target = menuTarget;
		it.enabled = (flags & 1) != 0;
		[m addItem:it];
	}
	[m popUpMenuPositioningItem:nil atLocation:[NSEvent mouseLocation] inView:nil];
}

// ---------------------------------------------------------------- 文件操作

char *dlTrash(const char *path) {
	NSError *err = nil;
	NSURL *u = [NSURL fileURLWithPath:dlStr(path)];
	if ([[NSFileManager defaultManager] trashItemAtURL:u resultingItemURL:nil error:&err]) return NULL;
	NSString *msg;
	switch (err.code) {
	case NSFileNoSuchFileError:
	case NSFileReadNoSuchFileError:
		msg = @"已经不存在了";
		break;
	case NSFileWriteNoPermissionError:
		msg = @"没有权限。它属于系统或者其他用户，或者受 macOS 保护。";
		break;
	case NSFileWriteVolumeReadOnlyError:
		msg = @"这个磁盘是只读的（比如 NTFS 格式的移动硬盘，Mac 只能读不能写）。";
		break;
	default:
		msg = err.localizedDescription ? err.localizedDescription : @"未知错误";
	}
	return strdup(msg.UTF8String);
}

bool dlOpenFile(const char *path) { return [[NSWorkspace sharedWorkspace] openURL:[NSURL fileURLWithPath:dlStr(path)]]; }

void dlReveal(const char *path) { [[NSWorkspace sharedWorkspace] activateFileViewerSelectingURLs:@[ [NSURL fileURLWithPath:dlStr(path)] ]]; }

void dlCopyText(const char *text) {
	NSPasteboard *pb = [NSPasteboard generalPasteboard];
	[pb clearContents];
	[pb setString:dlStr(text) forType:NSPasteboardTypeString];
}

void dlOpenURL(const char *url) { [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:dlStr(url)]]; }

// dlListVolumes 列出起始页上的磁盘：启动磁盘和 /Volumes 下的移动硬盘、U 盘、网络位置
DLVolume *dlListVolumes(int *n) {
	@autoreleasepool {
		NSArray *keys = @[
			NSURLVolumeLocalizedNameKey, NSURLVolumeTotalCapacityKey, NSURLVolumeAvailableCapacityKey, NSURLVolumeAvailableCapacityForImportantUsageKey,
			NSURLVolumeIsRemovableKey, NSURLVolumeIsEjectableKey, NSURLVolumeIsInternalKey, NSURLVolumeIsLocalKey, NSURLVolumeLocalizedFormatDescriptionKey,
			NSURLVolumeIsRootFileSystemKey, NSURLVolumeIsBrowsableKey
		];
		NSArray<NSURL *> *urls = [[NSFileManager defaultManager] mountedVolumeURLsIncludingResourceValuesForKeys:keys options:NSVolumeEnumerationSkipHiddenVolumes];
		DLVolume *out = calloc(urls.count + 1, sizeof(DLVolume));
		int k = 0;
		for (NSURL *u in urls) {
			NSString *p = u.path;
			if (!([p isEqualToString:@"/"] || [p hasPrefix:@"/Volumes/"])) continue;
			if ([p containsString:@"com.apple.TimeMachine"]) continue;
			NSDictionary *v = [u resourceValuesForKeys:keys error:nil];
			if (v[NSURLVolumeIsBrowsableKey] && ![v[NSURLVolumeIsBrowsableKey] boolValue]) continue;
			NSString *name = v[NSURLVolumeLocalizedNameKey] ?: p.lastPathComponent;
			NSString *fmt = v[NSURLVolumeLocalizedFormatDescriptionKey] ?: @"";
			out[k].path = strdup(p.fileSystemRepresentation);
			out[k].name = strdup(name.UTF8String);
			out[k].format = strdup(fmt.UTF8String);
			out[k].total = [v[NSURLVolumeTotalCapacityKey] longLongValue];
			out[k].avail = [v[NSURLVolumeAvailableCapacityKey] longLongValue];
			out[k].important = [v[NSURLVolumeAvailableCapacityForImportantUsageKey] longLongValue];
			out[k].root = [p isEqualToString:@"/"];
			out[k].removable = [v[NSURLVolumeIsRemovableKey] boolValue] || [v[NSURLVolumeIsEjectableKey] boolValue];
			out[k].internal = [v[NSURLVolumeIsInternalKey] boolValue];
			out[k].local = v[NSURLVolumeIsLocalKey] ? [v[NSURLVolumeIsLocalKey] boolValue] : true;
			k++;
		}
		*n = k;
		return out;
	}
}

void dlFreeVolumes(DLVolume *v, int n) {
	for (int i = 0; i < n; i++) {
		free(v[i].path);
		free(v[i].name);
		free(v[i].format);
	}
	free(v);
}
