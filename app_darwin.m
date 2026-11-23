// Orbit for macOS — the Cocoa side: the menu-bar item, one borderless window per widget (its
// picture is a CALayer, composited by the GPU), screen and mouse information, dates.
#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"
#include "gdip_darwin.h"
#include "app_darwin.h"

static NSStatusItem *statusItem;
static NSMenu *menu;
static NSTimer *tickTimer;
static double tickHz;

/* ---------- screens (Orbit works in pixels, y down from the top of the main screen) ---------- */

static NSScreen *mainScreen(void) { return NSScreen.screens.count ? NSScreen.screens[0] : NSScreen.mainScreen; }
double os_scale(void) { return mainScreen().backingScaleFactor ?: 1; }
static double mainTop(void) { return NSMaxY(mainScreen().frame); }

static void toPx(NSRect r, double *out) {
	double s = os_scale();
	out[0] = r.origin.x * s;
	out[1] = (mainTop() - NSMaxY(r)) * s;
	out[2] = r.size.width * s;
	out[3] = r.size.height * s;
}

void os_work_area(double *out) { toPx(mainScreen().visibleFrame, out); }

int os_screens(double *out, int max) {
	int n = 0;
	for (NSScreen *sc in NSScreen.screens) {
		if (n >= max) break;
		toPx(sc.frame, out + 4 * n);
		n++;
	}
	return n;
}

void os_cursor(double *x, double *y) {
	NSPoint p = NSEvent.mouseLocation;
	double s = os_scale();
	*x = p.x * s;
	*y = (mainTop() - p.y) * s;
}

int os_mouse_down(void) { return (NSEvent.pressedMouseButtons & 1) != 0; }

/* ---------- widget windows ---------- */

@interface OrbitWidgetView : NSView
@property int wid;
@end

@implementation OrbitWidgetView
- (BOOL)acceptsFirstMouse:(NSEvent *)e { return YES; }
- (BOOL)isFlipped { return YES; }
- (void)mouseDown:(NSEvent *)e { goWidgetMouse(self.wid, e.clickCount >= 2 ? 6 : 1, 0); }
- (void)mouseDragged:(NSEvent *)e { goWidgetMouse(self.wid, 2, 0); }
- (void)mouseUp:(NSEvent *)e { goWidgetMouse(self.wid, 3, 0); }
- (void)rightMouseUp:(NSEvent *)e { goWidgetMouse(self.wid, 5, 0); }
- (void)scrollWheel:(NSEvent *)e {
	double d = e.hasPreciseScrollingDeltas ? e.scrollingDeltaY * 4 : e.scrollingDeltaY * 40;
	if (fabs(d) >= 1) goWidgetMouse(self.wid, 4, (int)d);
}
- (void)resetCursorRects { [self addCursorRect:self.bounds cursor:NSCursor.openHandCursor]; }
@end

@interface OrbitPanel : NSPanel
@end
@implementation OrbitPanel
- (BOOL)canBecomeKeyWindow { return NO; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

void *ow_new(int wid) {
	OrbitPanel *w = [[OrbitPanel alloc] initWithContentRect:NSMakeRect(0, 0, 10, 10)
		styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:YES];
	w.opaque = NO;
	w.backgroundColor = NSColor.clearColor;
	w.hasShadow = NO;
	w.ignoresMouseEvents = YES;
	w.releasedWhenClosed = NO;
	w.hidesOnDeactivate = NO;
	w.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary | NSWindowCollectionBehaviorIgnoresCycle | NSWindowCollectionBehaviorFullScreenAuxiliary;
	OrbitWidgetView *v = [[OrbitWidgetView alloc] initWithFrame:NSMakeRect(0, 0, 10, 10)];
	v.wid = wid;
	v.wantsLayer = YES;
	v.layer.contentsGravity = kCAGravityResize;
	v.layerContentsRedrawPolicy = NSViewLayerContentsRedrawNever;
	w.contentView = v;
	return (__bridge_retained void *)w;
}

// level: 0 on the desktop (above Mac desktop widgets and icons, under every app window),
// 1 always on top, 2 over everything (screen glow, the arrange banner)
void ow_level(void *wp, int level) {
	NSWindow *w = (__bridge NSWindow *)wp;
	NSInteger l = level == 0 ? kCGNormalWindowLevel - 1 : level == 1 ? NSFloatingWindowLevel : NSStatusWindowLevel;
	if (w.level != l) w.level = l;
}

// ow_show: put the canvas on screen at (x, y, w, h) pixels, with an opacity
void ow_show(void *wp, OG *g, double x, double y, double w, double h, double alpha) {
	NSWindow *win = (__bridge NSWindow *)wp;
	CGImageRef img = (CGImageRef)og_cgimage(g);
	if (!img) return;
	double s = os_scale();
	NSRect fr = NSMakeRect(x / s, mainTop() - (y + h) / s, w / s, h / s);
	[CATransaction begin];
	[CATransaction setDisableActions:YES]; // no implicit animation between frames
	NSView *v = win.contentView;
	v.layer.contents = (__bridge id)img;
	v.layer.contentsScale = s;
	[CATransaction commit];
	CGImageRelease(img);
	if (!NSEqualRects(win.frame, fr)) [win setFrame:fr display:NO];
	if (win.alphaValue != alpha) win.alphaValue = alpha;
	if (!win.isVisible) [win orderFrontRegardless];
}

void ow_move(void *wp, double x, double y) {
	NSWindow *win = (__bridge NSWindow *)wp;
	double s = os_scale();
	[win setFrameTopLeftPoint:NSMakePoint(x / s, mainTop() - y / s)];
}

void ow_rect(void *wp, double *out) { toPx(((__bridge NSWindow *)wp).frame, out); }
void ow_hide(void *wp) { [(__bridge NSWindow *)wp orderOut:nil]; }
void ow_mouse(void *wp, int on) { ((__bridge NSWindow *)wp).ignoresMouseEvents = !on; }
void ow_close(void *wp) { NSWindow *w = (__bridge_transfer NSWindow *)wp; [w orderOut:nil]; [w close]; }

/* ---------- dates in the user's language ---------- */

int os_format_date(long long unix, int lng, char *buf, int n) {
	NSDateFormatter *f = [NSDateFormatter new];
	f.locale = NSLocale.currentLocale;
	f.dateFormat = [NSDateFormatter dateFormatFromTemplate:(lng ? @"EEEEdMMMM" : @"EEEdMMM") options:0 locale:NSLocale.currentLocale];
	NSString *s = [f stringFromDate:[NSDate dateWithTimeIntervalSince1970:unix]];
	const char *u = s.UTF8String;
	if (!u) return 0;
	strncpy(buf, u, n - 1);
	buf[n - 1] = 0;
	return (int)strlen(buf);
}

/* ---------- the menu-bar item ---------- */

@interface OrbitDelegate : NSObject <NSApplicationDelegate, NSMenuDelegate>
@end
@implementation OrbitDelegate
- (void)menu:(NSMenuItem *)m { goMenu((int)m.tag); }
- (void)tick:(NSTimer *)t { goTick(); }
- (void)menuNeedsUpdate:(NSMenu *)m { goMenuOpening(); }
@end
static OrbitDelegate *delegate;

void os_menu_clear(void) { [menu removeAllItems]; }
void os_menu_add(const char *title, int tag, int checked, int enabled) {
	if (tag < 0) { [menu addItem:[NSMenuItem separatorItem]]; return; }
	NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:[NSString stringWithUTF8String:title] action:@selector(menu:) keyEquivalent:@""];
	it.target = delegate;
	it.tag = tag;
	it.state = checked ? NSControlStateValueOn : NSControlStateValueOff;
	it.enabled = enabled != 0;
	[menu addItem:it];
}

// how often Go is asked for a frame: the chosen fps while something moves, a few when idle
void os_set_rate(double hz) {
	if (hz < 1) hz = 1;
	if (fabs(hz - tickHz) < 0.01) return;
	tickHz = hz;
	[tickTimer invalidate];
	tickTimer = [NSTimer timerWithTimeInterval:1.0 / hz target:delegate selector:@selector(tick:) userInfo:nil repeats:YES];
	tickTimer.tolerance = 0.1 / hz; // lets macOS group wake-ups
	[[NSRunLoop mainRunLoop] addTimer:tickTimer forMode:NSRunLoopCommonModes];
}

void os_run(const char *iconPath) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]; // menu bar only, no Dock icon
		delegate = [OrbitDelegate new];
		NSApp.delegate = delegate;
		statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
		NSImage *img = iconPath && *iconPath ? [[NSImage alloc] initWithContentsOfFile:[NSString stringWithUTF8String:iconPath]] : nil;
		if (img) {
			img.size = NSMakeSize(18, 18);
			img.template = YES; // tinted for light and dark menu bars
			statusItem.button.image = img;
		} else {
			statusItem.button.title = @"◉";
		}
		statusItem.button.toolTip = @"Orbit";
		menu = [NSMenu new];
		menu.autoenablesItems = NO;
		menu.delegate = delegate;
		statusItem.menu = menu;
		goStarted();
		for (NSString *name in @[@"com.spotify.client.PlaybackStateChanged", @"com.apple.Music.playerInfo"]) {
			[[NSDistributedNotificationCenter defaultCenter] addObserverForName:name object:nil queue:nil usingBlock:^(NSNotification *n) { goMediaPing(); }];
		}
		[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidChangeScreenParametersNotification object:nil queue:nil usingBlock:^(NSNotification *n) { goScreensChanged(); }];
		os_set_rate(2);
		[NSApp run];
	}
}

void os_set_tooltip(const char *s) { statusItem.button.toolTip = [NSString stringWithUTF8String:s]; }
void os_quit(void) { dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; }); }

// os_desktop_at: nothing from another app covers this point (pixels) — the widget under the
// mouse can be hovered.
int os_desktop_at(double x, double y) {
	double s = os_scale();
	CGPoint p = CGPointMake(x / s, y / s); // window bounds: points, y down from the main screen's top
	CFArrayRef list = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
	if (!list) return 1;
	int me = getpid(), free_ = 1;
	for (CFIndex i = 0; i < CFArrayGetCount(list); i++) {
		NSDictionary *w = (__bridge NSDictionary *)CFArrayGetValueAtIndex(list, i);
		if ([w[(id)kCGWindowOwnerPID] intValue] == me) continue;
		int layer = [w[(id)kCGWindowLayer] intValue];
		if (layer != 0) continue; // menu bar, Dock, overlays
		CGRect r;
		if (!CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)w[(id)kCGWindowBounds], &r)) continue;
		if (CGRectContainsPoint(r, p)) { free_ = 0; break; }
	}
	CFRelease(list);
	return free_;
}
