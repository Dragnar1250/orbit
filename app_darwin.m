// Orbit for macOS — the Cocoa side: the menu-bar item, one borderless window per widget (its
// picture is a CALayer, composited by the GPU), screen and mouse information, dates.
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <WebKit/WebKit.h>
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

/* ---------- the settings window: the same page as on Windows, in Orbit's own window ---------- */

static NSWindow *settingsWin;

@interface OrbitSettingsDelegate : NSObject <NSWindowDelegate, WKUIDelegate, WKNavigationDelegate>
@end
@implementation OrbitSettingsDelegate
- (void)windowWillClose:(NSNotification *)n {
	// give the page's memory back and leave the Dock again
	settingsWin.contentView = nil;
	settingsWin = nil;
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
// links that open a new window (Spotify sign-in, help pages): the default browser
- (WKWebView *)webView:(WKWebView *)w createWebViewWithConfiguration:(WKWebViewConfiguration *)c forNavigationAction:(WKNavigationAction *)a windowFeatures:(WKWindowFeatures *)f {
	if (a.request.URL) [[NSWorkspace sharedWorkspace] openURL:a.request.URL];
	return nil;
}
- (void)webView:(WKWebView *)w decidePolicyForNavigationAction:(WKNavigationAction *)a decisionHandler:(void (^)(WKNavigationActionPolicy))h {
	NSURL *u = a.request.URL;
	if (u && ![u.host isEqualToString:@"127.0.0.1"] && ![u.scheme isEqualToString:@"about"]) {
		[[NSWorkspace sharedWorkspace] openURL:u]; // anything outside Orbit: the browser
		h(WKNavigationActionPolicyCancel);
		return;
	}
	h(WKNavigationActionPolicyAllow);
}
- (void)webView:(WKWebView *)w runJavaScriptAlertPanelWithMessage:(NSString *)m initiatedByFrame:(WKFrameInfo *)f completionHandler:(void (^)(void))h {
	NSAlert *al = [NSAlert new];
	al.messageText = m;
	[al runModal];
	h();
}
- (void)webView:(WKWebView *)w runJavaScriptConfirmPanelWithMessage:(NSString *)m initiatedByFrame:(WKFrameInfo *)f completionHandler:(void (^)(BOOL))h {
	NSAlert *al = [NSAlert new];
	al.messageText = m;
	[al addButtonWithTitle:@"OK"];
	[al addButtonWithTitle:@"Cancel"];
	h([al runModal] == NSAlertFirstButtonReturn);
}
@end
static OrbitSettingsDelegate *settingsDelegate;

void os_open_settings(const char *url) {
	NSURL *u = [NSURL URLWithString:[NSString stringWithUTF8String:url]];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular]; // a normal app with a Dock icon while it is open
	if (settingsWin) {
		WKWebView *wv = (WKWebView *)settingsWin.contentView;
		if ([wv isKindOfClass:[WKWebView class]] && u.fragment.length) [wv loadRequest:[NSURLRequest requestWithURL:u]];
		[settingsWin makeKeyAndOrderFront:nil];
		[NSApp activateIgnoringOtherApps:YES];
		return;
	}
	if (!settingsDelegate) settingsDelegate = [OrbitSettingsDelegate new];
	settingsWin = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 1180, 820)
		styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
		backing:NSBackingStoreBuffered defer:NO];
	settingsWin.title = @"Orbit";
	settingsWin.releasedWhenClosed = NO;
	settingsWin.minSize = NSMakeSize(720, 520);
	settingsWin.delegate = settingsDelegate;
	settingsWin.tabbingMode = NSWindowTabbingModeDisallowed;
	[settingsWin setFrameAutosaveName:@"OrbitSettings"];
	WKWebViewConfiguration *c = [WKWebViewConfiguration new];
	c.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
	WKWebView *wv = [[WKWebView alloc] initWithFrame:settingsWin.contentView.bounds configuration:c];
	wv.UIDelegate = settingsDelegate;
	wv.navigationDelegate = settingsDelegate;
	wv.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	[wv setValue:@NO forKey:@"drawsBackground"];
	settingsWin.contentView = wv;
	[wv loadRequest:[NSURLRequest requestWithURL:u]];
	if (!NSEqualPoints(settingsWin.frame.origin, NSZeroPoint)) {} else [settingsWin center];
	[settingsWin makeKeyAndOrderFront:nil];
	[NSApp activateIgnoringOtherApps:YES];
}

int os_settings_open(void) { return settingsWin != nil; }

@interface OrbitDelegate : NSObject <NSApplicationDelegate, NSMenuDelegate>
@end
@implementation OrbitDelegate
- (void)menu:(NSMenuItem *)m { goMenu((int)m.tag); }
- (void)tick:(NSTimer *)t { goTick(); }
- (void)menuNeedsUpdate:(NSMenu *)m { goMenuOpening(); }
// double-clicking Orbit.app again (or its Dock icon) opens the settings
- (BOOL)applicationShouldHandleReopen:(NSApplication *)a hasVisibleWindows:(BOOL)v { goMenu(1); return NO; }
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)a { return NO; }
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

// os_take_over: only one Orbit at a time — an Orbit already running (an older copy, another
// place on disk, a login item) is asked to quit, and stopped if it doesn't within 3 s.
int os_take_over(void) {
	int closed = 0;
	pid_t me = getpid();
	NSString *bid = NSBundle.mainBundle.bundleIdentifier ?: @"com.dragnar1250.orbit";
	NSMutableArray *others = [NSMutableArray array];
	for (NSRunningApplication *a in [NSRunningApplication runningApplicationsWithBundleIdentifier:bid])
		if (a.processIdentifier != me) [others addObject:a];
	for (NSRunningApplication *a in NSWorkspace.sharedWorkspace.runningApplications) // copies started outside the app bundle
		if (a.processIdentifier != me && [a.executableURL.lastPathComponent isEqualToString:@"Orbit"] &&
			(!a.bundleIdentifier || [a.bundleIdentifier.lowercaseString containsString:@"orbit"]) && ![others containsObject:a]) [others addObject:a];
	for (NSRunningApplication *a in others) { [a terminate]; closed++; }
	for (int i = 0; i < 30 && others.count; i++) {
		[NSThread sleepForTimeInterval:0.1];
		NSMutableArray *left = [NSMutableArray array];
		for (NSRunningApplication *a in others) if (!a.terminated) [left addObject:a];
		others = left;
	}
	for (NSRunningApplication *a in others) [a forceTerminate];
	return closed;
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
		// clicks and scrolls on the desktop (the widgets let them through): play / pause by
		// clicking the record, double-click to change colours, scroll to resize — like on Windows
		[NSEvent addGlobalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown | NSEventMaskLeftMouseUp | NSEventMaskScrollWheel handler:^(NSEvent *e) {
			double x, y;
			os_cursor(&x, &y);
			int kind = e.type == NSEventTypeLeftMouseDown ? 1 : e.type == NSEventTypeLeftMouseUp ? 3 : 4;
			int d = 0;
			if (kind == 4) d = (int)(e.hasPreciseScrollingDeltas ? e.scrollingDeltaY * 4 : e.scrollingDeltaY * 40);
			goGlobalMouse(kind, (int)x, (int)y, d, (int)e.clickCount);
		}];
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
static double lastAtT, lastAtX = -1e9, lastAtY = -1e9;
static int lastAt;

int os_desktop_at(double x, double y) {
	// asking the window server is costly: the answer for the same spot is kept for 250 ms
	double now = CFAbsoluteTimeGetCurrent();
	if (fabs(x - lastAtX) < 3 && fabs(y - lastAtY) < 3 && now - lastAtT < 0.25) return lastAt;
	lastAtX = x; lastAtY = y; lastAtT = now;
	lastAt = os_desktop_at_now(x, y);
	return lastAt;
}

int os_desktop_at_now(double x, double y) {
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

double os_double_click_time(void) { return NSEvent.doubleClickInterval; }
