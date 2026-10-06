// Mac 版界面（AppKit，Objective-C）和 Go 之间的接口。
// Go 这边负责扫描、排版和所有的逻辑，这边只管窗口、控件和画图。
// 下面以 dl 开头的函数由 Go 调用（实现在各个 *_darwin.m 里），go 开头的是 Go 导出给这边回调的。
// 所有 dl 函数都只能在主线程上调用（dlPost、dlListVolumes 除外）。

#ifndef DISKLENS_COCOA_H
#define DISKLENS_COCOA_H

#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

// 命令编号，和 app_darwin.go 里的 mc* 常量一一对应
enum {
	DL_CMD_HOME = 1,      // 换个磁盘（回起始页）／回到扫描结果
	DL_CMD_BACK = 3,
	DL_CMD_FORWARD = 4,
	DL_CMD_UP = 5,
	DL_CMD_STOP_OR_RESCAN = 6,
	DL_CMD_RESCAN_ALL = 7,
	DL_CMD_STOP = 8,
	DL_CMD_SIDE = 9,
	DL_CMD_PICK = 10,
	DL_CMD_DETAIL0 = 11, // 11、12、13：粗略、适中、精细
	DL_CMD_OPEN = 20,    // 打开选中的（文件夹就进去）
	DL_CMD_REVEAL = 21,
	DL_CMD_COPY_PATH = 22,
	DL_CMD_TRASH = 23,
	DL_CMD_DELETE = 24,
	DL_CMD_RESCAN_ITEM = 25,
	DL_CMD_DESELECT = 27,
	DL_CMD_FDA = 30,  // 完全磁盘访问权限的说明
	DL_CMD_HELP = 31,
	DL_CMD_ABOUT = 32,
};

// 树图鼠标事件
enum {
	DL_MOUSE_MOVE = 0,
	DL_MOUSE_DOWN = 1, // 左键，clicks = 1 单击、2 双击
	DL_MOUSE_RIGHT = 2,
	DL_MOUSE_EXIT = 3,
	DL_MOUSE_BACK = 4, // 鼠标侧键
	DL_MOUSE_FORWARD = 5,
};

// 树图里的一块（像素坐标，相对树图左上角，y 向下）
typedef struct {
	int32_t x0, y0, x1, y1;     // 方块
	int32_t ix0, iy0, ix1, iy1; // 文件夹里放子方块的区域
	int32_t kind;               // 0 文件夹、1 文件、2 一堆小文件
	int32_t flags;              // 见 DL_TILE_*
	uint32_t color;             // 文件的颜色 0xRRGGBB
	int32_t name, nameLen;      // 名字在文字块里的位置（UTF-8 字节）
	int32_t size, sizeLen;      // 大小（「1.2 GB」「1.2 GB · 扫描中」）
} DLTile;

enum {
	DL_TILE_HEADER = 1,   // 文件夹有标题栏
	DL_TILE_KIDS = 2,     // 文件夹里画了子方块
	DL_TILE_SCANNING = 4, // 文件夹还在扫描（大小用强调色）
	DL_TILE_EVEN = 8,     // 层数是偶数（标题栏换一种底色）
};

// 右侧列表的一行；字符串由 Go 用 malloc 分配，这边用完 free
typedef struct {
	char *name, *size, *files, *path;
	double share;  // 占当前文件夹的比例
	int32_t flags; // 1 文件夹、2 没有权限、4 还在扫描
} DLRow;

// 起始页上的一个磁盘
typedef struct {
	char *path, *title, *badge, *sub;
	double used;
	bool low;
} DLCard;

// 读出来的一个宗卷（dlListVolumes 返回，调用方 dlFreeVolumes）
typedef struct {
	char *path, *name, *format;
	int64_t total, avail, important;
	bool root, removable, internal, local;
} DLVolume;

// ---------------------------------------------------------------- Go 调用

// shot 为 true 时是截图模式：窗口固定大小、不记位置，树图按 2 倍画
void dlRun(bool shot);
void dlTerminate(void);
void dlPost(int64_t tag);      // 任何线程都可以调；回到主线程后调 goPosted(tag)
void dlAfter(int ms, int64_t tag); // ms 毫秒后在主线程上调 goPosted(tag)
void dlStartTimer(void);      // 每 100 毫秒调一次 goTick
void dlStopTimer(void);
void dlRunLoopFor(int ms);    // 截图模式：让界面跑一会儿

void dlSetMode(int mode); // 0 起始页、1 查看
void dlSetTitle(const char *title, const char *subtitle);
// 工具栏：home 按钮文字（空字符串 = 禁用）、能不能后退前进上一级、是否在忙（停止按钮、转圈）、
// 重新扫描能不能点、细节档位、右侧列表开没开
void dlSetToolbar(const char *home, bool back, bool fwd, bool up, bool busy, bool rescan, int detail, bool side, bool view);
void dlSetCrumbs(const char *names, const char *paths); // 用 \n 分隔
void dlSetFooter(const char *text);
void dlSetLegend(const char *names, const uint32_t *colors, int n); // 底栏的颜色图例，names 用 \n 分隔
void dlToast(const char *msg, bool isErr);

void dlSetCards(DLCard *cards, int n, bool loading);
void dlSetHomeTip(bool show);
DLVolume *dlListVolumes(int *n); // 任何线程都可以调
void dlFreeVolumes(DLVolume *v, int n);

void dlMapSize(int32_t *w, int32_t *h, double *scale);
void dlMapSetTiles(DLTile *tiles, int n, char *text, bool scanning, double scale); // 接管 tiles 和 text（malloc 的）
void dlMapSetMarks(bool sel, int32_t sx0, int32_t sy0, int32_t sx1, int32_t sy1, bool hover, int32_t hx0, int32_t hy0, int32_t hx1, int32_t hy1);
// 鼠标指着的方块的提示框；name 为 NULL 时隐藏。lines 用 \n 分隔
void dlMapSetTip(const char *name, const char *size, const char *share, const char *lines, const char *path, const char *hint);
void dlMapSetMessage(const char *msg);
void dlMapShotMouse(double x, double y); // 截图模式：假装鼠标在树图的这个位置（点）
void dlMapZoom(int32_t fx0, int32_t fy0, int32_t fx1, int32_t fy1, int32_t tx0, int32_t ty0, int32_t tx1, int32_t ty1, bool useOld);

void dlListReload(int n, bool newFolder);
void dlListSelect(int row, bool scroll);
void dlSetSide(const char *name, const char *size, const char *meta, const char *note, const char *link, const char *iconPath, bool visible);

// 对话框：style 0 普通、1 警告、2 错误；buttons 用 \n 分隔，第一个在最右边；
// def 是按回车触发的按钮，destructive 是红色的危险按钮（-1 没有）；
// check 不为空时加一个勾选框，needCheck 号按钮要勾上才能点；details 不为空时下面放一个可以滚动的文本框。
// 返回点的按钮序号，*checked 是勾选框的状态。
// shotFile 不为空时（截图模式）：0.8 秒后把对话框截图存到这里，然后（勾上勾选框并）点 shotClick 号按钮。
int dlAlert(int style, const char *title, const char *msg, const char *buttons, int def, int destructive,
            const char *check, int needCheck, const char *details, bool *checked,
            const char *shotFile, int shotClick, bool shotCheck);
void dlPickFolder(void); // 选好后调 goOpenPath
// 在鼠标位置弹出右键菜单：每行「编号\t标志\t快捷键\t文字」，标志 1 可用、2 分隔线、4 ⌘、8 ⌥、16 ⇧。
// 选了哪一项通过 goMenuPick(编号) 告诉 Go
void dlPopupMenu(const char *items);

char *dlTrash(const char *path); // 成功返回 NULL，失败返回 malloc 的错误说明
bool dlOpenFile(const char *path);
void dlReveal(const char *path);
void dlCopyText(const char *text);
void dlOpenURL(const char *url);
bool dlCapture(const char *file); // 把窗口画成 PNG（截图模式）
void dlLog(const char *msg);       // 写到标准输出（截图模式的日志）

#endif
