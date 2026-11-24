// Orbit for macOS — the Cocoa side: the menu-bar item, one borderless window per widget (its
// picture is a CALayer, composited by the GPU), screen and mouse information, dates.
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <WebKit/WebKit.h>
#import <IOSurface/IOSurface.h>
#import <CoreVideo/CoreVideo.h>
#include <signal.h>
#include <stdatomic.h>
#include "_cgo_export.h"
#include "gdip_darwin.h"
#include "app_darwin.h"

static NSStatusItem *statusItem;
static NSMenu *menu;
static NSTimer *tickTimer;
static double tickHz;

/* ---------- screens (Orbit works in pixels, y down from the top of the main screen) ---------- */

static NSScreen *mainScreen(void) { return NSScreen.screens.count ? NSScreen.screens[0] : NSScreen.mainScreen; }

// the screen's numbers are asked for many times a frame; asking AppKit each time showed up as a
// fifth of the CPU — kept here, renewed when the screens change (and every 2 s, to be safe)
static double scrScale = 0, scrTop, scrAt;
static NSRect scrVisible;
static void screenInfo(void) {
	double now = CFAbsoluteTimeGetCurrent();
	if (scrScale > 0 && now - scrAt < 2) return;
	scrAt = now;
	NSScreen *m = mainScreen();
	scrScale = m.backingScaleFactor ?: 1;
	scrTop = NSMaxY(m.frame);
	scrVisible = m.visibleFrame;
}
void os_screens_moved(void) { scrScale = 0; }

double os_scale(void) {
	static double forced = -1; // ORBIT_SCALE=2: draw as many pixels as a Retina screen (tests)
	if (forced < 0) { const char *e = getenv("ORBIT_SCALE"); forced = e ? atof(e) : 0; }
	if (forced > 0) return forced;
	screenInfo();
	return scrScale;
}
static double mainTop(void) { screenInfo(); return scrTop; }

static void toPx(NSRect r, double *out) {
	double s = os_scale();
	out[0] = r.origin.x * s;
	out[1] = (mainTop() - NSMaxY(r)) * s;
	out[2] = r.size.width * s;
	out[3] = r.size.height * s;
}

void os_work_area(double *out) { screenInfo(); toPx(scrVisible, out); }

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


/* ---------- showing a canvas: IOSurfaces the window server reads directly ---------- */

// A CGImage per frame meant a copy-on-write copy of the canvas, a colour conversion and an upload
// on every frame. An IOSurface is memory the GPU and the window server share: the canvas is copied
// into it once (a plain memcpy) and the layer shows it as it is. Three of them in turn, so the one
// being written is never the one on screen.
@interface OrbitSurfaces : NSObject {
@public
	IOSurfaceRef s[3];
	int w, h, cur;
}
@end
long livePresentBytes;
@implementation OrbitSurfaces
- (void)drop { for (int i = 0; i < 3; i++) if (s[i]) { livePresentBytes -= (long)IOSurfaceGetAllocSize(s[i]); CFRelease(s[i]); s[i] = NULL; } w = h = 0; }
- (void)dealloc { [self drop]; }
@end

static IOSurfaceRef newSurface(int w, int h) {
	NSDictionary *p = @{
		(id)kIOSurfaceWidth: @(w), (id)kIOSurfaceHeight: @(h), (id)kIOSurfaceBytesPerElement: @4,
		(id)kIOSurfacePixelFormat: @((unsigned)'BGRA'),
	};
	return IOSurfaceCreate((__bridge CFDictionaryRef)p);
}

static int presentCount;
static double presentSecs;
double showSplit[4]; // (perf) unlock, contents, window, total — seconds since the last report

int os_present_stats(double *secs) { int n = presentCount; *secs = presentSecs; presentCount = 0; presentSecs = 0; return n; }

// presentOn: put canvas g on layer l (all of it, or only the rows y0..y1 changed since the last frame)
static void presentOn(CALayer *l, OrbitSurfaces *ps, OG *g, double scale) {
	double t0 = CACurrentMediaTime();
	int w = og_width(g), h = og_height(g);
	if (ps->w != w || ps->h != h) {
		[ps drop];
		ps->w = w; ps->h = h;
	}
	int i = (ps->cur + 1) % 3;
	for (int k = 0; k < 3; k++, i = (i + 1) % 3) { // one the window server isn't reading
		if (!ps->s[i]) { ps->s[i] = newSurface(w, h); if (ps->s[i]) livePresentBytes += (long)IOSurfaceGetAllocSize(ps->s[i]); }
		if (ps->s[i] && !IOSurfaceIsInUse(ps->s[i])) break;
	}
	IOSurfaceRef sf = ps->s[i];
	if (!sf) return;
	IOSurfaceLock(sf, 0, NULL);
	uint8_t *dst = IOSurfaceGetBaseAddress(sf);
	size_t dstride = IOSurfaceGetBytesPerRow(sf), srow = (size_t)w * 4;
	const uint8_t *src = og_bits(g);
	if (dstride == srow) memcpy(dst, src, srow * h);
	else for (int y = 0; y < h; y++) memcpy(dst + y * dstride, src + y * srow, srow);
	IOSurfaceUnlock(sf, 0, NULL);
	ps->cur = i;
	l.contents = (__bridge id)sf; // (no implicit animation: the layers have none)
	if (l.contentsScale != scale) l.contentsScale = scale;
	presentCount++;
	presentSecs += CACurrentMediaTime() - t0;
}

void os_present_layer(void *layer, void **state, OG *g, double scale) {
	CALayer *l = (__bridge CALayer *)layer;
	if (!*state) *state = (__bridge_retained void *)[OrbitSurfaces new];
	presentOn(l, (__bridge OrbitSurfaces *)*state, g, scale);
}

void os_present_free(void **state) {
	if (*state) { OrbitSurfaces *ps = (__bridge_transfer OrbitSurfaces *)*state; [ps drop]; *state = NULL; }
}

/* ---------- widget windows ---------- */

// A widget's window holds up to four pictures, bottom to top: the soft layer (glows and light,
// drawn at a quarter of the pixels and stretched by the GPU), a still picture (the record without
// its label), the turning label (turned by the GPU), rings of light (the record's groove light,
// drawn by the GPU), the main picture and a still picture on top (the record's shine and arm).
enum { LSoft, LBase, LSpin, LRings, LMain, LTop, LGlowS, LTextS, LCount };

static CALayer *newLayer(void) {
	CALayer *l = [CALayer layer];
	NSNull *no = NSNull.null;
	l.actions = @{@"contents": no, @"bounds": no, @"position": no, @"transform": no, @"hidden": no, @"opacity": no, @"frame": no, @"contentsScale": no, @"anchorPoint": no, @"sublayers": no};
	l.contentsGravity = kCAGravityResize;
	l.anchorPoint = CGPointMake(0.5, 0.5);
	return l;
}

@interface OrbitWidgetView : NSView {
@public
	CALayer *layers[LCount];
	void *surfaces[LCount];
	int spinVer;
	NSRect asked; // the frame last asked for (AppKit rounds it: comparing with window.frame would
	              // move the window again every frame)
	double alpha;
}
@property int wid;
@end

@implementation OrbitWidgetView
- (BOOL)acceptsFirstMouse:(NSEvent *)e { return YES; }
- (BOOL)isFlipped { return NO; } // layers in plain y-up geometry: pictures upright, positions converted
- (CALayer *)part:(int)i {
	if (!layers[i]) {
		layers[i] = newLayer();
		layers[i].zPosition = i;
		layers[i].frame = self.layer.bounds;
		[self.layer addSublayer:layers[i]];
	}
	return layers[i];
}
- (void)drop:(int)i {
	if (layers[i]) { layers[i].contents = nil; layers[i].hidden = YES; }
	os_present_free(&surfaces[i]);
	if (i == LSpin) spinVer = 0;
}
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
	double s = os_scale();
	NSRect fr = NSMakeRect(x / s, mainTop() - (y + h) / s, w / s, h / s);
	OrbitWidgetView *v = (OrbitWidgetView *)win.contentView;
	double tw = CACurrentMediaTime();
	if (!NSEqualRects(v->asked, fr)) { v->asked = fr; [win setFrame:fr display:NO]; }
	// no explicit transaction: each would be its own trip to the window server; the run loop
	// sends every widget's change together after the frame
	CGRect b = CGRectMake(0, 0, w / s, h / s);
	for (int i = 0; i < LCount; i++)
		if (v->layers[i] && i != LSpin && !CGRectEqualToRect(v->layers[i].frame, b)) v->layers[i].frame = b;
	double t0 = CACurrentMediaTime(), t1 = t0, t2 = t0;
	if (g) {
		CALayer *m = [v part:LMain];
		if (m.hidden) m.hidden = NO;
		IOSurfaceRef sf = og_surface_show(g);
		t1 = CACurrentMediaTime();
		if (sf) { // drawn straight into the surface: just show it
			if (m.contents != (__bridge id)sf) m.contents = (__bridge id)sf;
			if (m.contentsScale != s) m.contentsScale = s;
			presentCount++;
		} else os_present_layer((__bridge void *)m, &v->surfaces[LMain], g, s);
		t2 = CACurrentMediaTime();
	} else [v drop:LMain]; // (all of it is on the soft layer)
	showSplit[0] += t1 - t0;
	showSplit[1] += t2 - t1;
	showSplit[2] += t0 - tw;
	if (v->alpha != alpha + 1) { v->alpha = alpha + 1; win.alphaValue = alpha; } // (+1: 0 is "not set yet")
	if (!win.isVisible) [win orderFrontRegardless];
	double t3 = CACurrentMediaTime();
	showSplit[2] += t3 - t2;
	showSplit[3] += t3 - tw;
}

void os_show_split(double *out) { for (int i = 0; i < 4; i++) { out[i] = showSplit[i]; showSplit[i] = 0; } }

// ow_layer: picture g (k times the window's pixels) as layer i; NULL: none
void ow_layer(void *wp, int i, OG *g, double k) {
	OrbitWidgetView *v = (OrbitWidgetView *)((__bridge NSWindow *)wp).contentView;
	if (!g) [v drop:i];
	else {
		CALayer *l = [v part:i];
		if (!CGRectEqualToRect(l.frame, v.layer.bounds)) l.frame = v.layer.bounds;
		l.hidden = NO;
		os_present_layer((__bridge void *)l, &v->surfaces[i], g, os_scale() * k);
	}
}

// ow_spin: picture g (sent again only when ver changes) centred at (cx, cy) pixels, turned by deg
void ow_spin(void *wp, OG *g, int ver, double cx, double cy, double deg) {
	OrbitWidgetView *v = (OrbitWidgetView *)((__bridge NSWindow *)wp).contentView;
	double s = os_scale();
	if (!g) [v drop:LSpin];
	else {
		CALayer *l = [v part:LSpin];
		l.hidden = NO;
		if (v->spinVer != ver) {
			v->spinVer = ver;
			os_present_layer((__bridge void *)l, &v->surfaces[LSpin], g, s);
			l.bounds = CGRectMake(0, 0, og_width(g) / s, og_height(g) / s);
		}
		l.position = CGPointMake(cx / s, v.layer.bounds.size.height - cy / s); // (y up here)
		l.transform = CATransform3DMakeRotation(-deg * M_PI / 180, 0, 0, 1); // clockwise, as on Windows
	}
}

void ow_move(void *wp, double x, double y) {
	NSWindow *win = (__bridge NSWindow *)wp;
	OrbitWidgetView *v = (OrbitWidgetView *)win.contentView;
	double s = os_scale();
	NSRect fr = v->asked;
	if (fr.size.width <= 0) fr.size = win.frame.size;
	fr.origin = NSMakePoint(x / s, mainTop() - y / s - fr.size.height);
	if (NSEqualRects(v->asked, fr)) return;
	v->asked = fr;
	[win setFrame:fr display:NO];
}

void ow_rect(void *wp, double *out) { toPx(((__bridge NSWindow *)wp).frame, out); }
void ow_hide(void *wp) {
	NSWindow *w = (__bridge NSWindow *)wp;
	[w orderOut:nil];
	OrbitWidgetView *v = (OrbitWidgetView *)w.contentView;
	for (int i = 0; i < LCount; i++) [v drop:i]; // hidden widgets keep no pictures
	v->alpha = 0;
}
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

// how often Go is asked for a frame: the chosen fps while something moves, a few when idle.
// Frames come from the display's own refresh (CVDisplayLink), every n-th one for the chosen fps,
// so they are evenly spaced and land just before the screen is redrawn — a timer drifts against
// the refresh and every so often two frames land in one refresh and none in the next, which
// looks like stutter. A slow timer is used when little moves (fewer wake-ups).
static CVDisplayLinkRef dispLink;
static _Atomic int linkWant, linkPending, linkDiv, linkCount;
static double linkRefresh = 60;

static CVReturn linkFired(CVDisplayLinkRef l, const CVTimeStamp *now, const CVTimeStamp *out, CVOptionFlags in, CVOptionFlags *flags, void *ctx) {
	if (!atomic_load(&linkWant)) return kCVReturnSuccess;
	int n = atomic_fetch_add(&linkCount, 1) + 1;
	if (n < atomic_load(&linkDiv)) return kCVReturnSuccess;
	if (atomic_exchange(&linkPending, 1)) return kCVReturnSuccess; // the last frame is still being drawn: skip, don't queue up
	atomic_store(&linkCount, 0);
	dispatch_async(dispatch_get_main_queue(), ^{
		atomic_store(&linkPending, 0);
		goTick();
	});
	return kCVReturnSuccess;
}

static void linkStart(double hz) {
	if (!dispLink) {
		if (CVDisplayLinkCreateWithActiveCGDisplays(&dispLink) != kCVReturnSuccess) { dispLink = NULL; return; }
		CVDisplayLinkSetOutputCallback(dispLink, linkFired, NULL);
	}
	CVDisplayLinkSetCurrentCGDisplay(dispLink, CGMainDisplayID());
	double per = CVDisplayLinkGetActualOutputVideoRefreshPeriod(dispLink);
	if (per <= 0) {
		CVTime t = CVDisplayLinkGetNominalOutputVideoRefreshPeriod(dispLink);
		if (t.timeValue > 0) per = (double)t.timeValue / t.timeScale;
	}
	if (per > 0) linkRefresh = 1 / per;
	int div = (int)lround(linkRefresh / hz);
	atomic_store(&linkDiv, div < 1 ? 1 : div);
	atomic_store(&linkWant, 1);
	if (!CVDisplayLinkIsRunning(dispLink)) CVDisplayLinkStart(dispLink);
}

static void linkStop(void) {
	atomic_store(&linkWant, 0);
	if (dispLink && CVDisplayLinkIsRunning(dispLink)) CVDisplayLinkStop(dispLink);
}

double os_refresh_rate(void) { return linkRefresh; }
double os_tick_hz(void) { return tickHz; }

void os_set_rate(double hz) {
	if (hz < 1) hz = 1;
	if (fabs(hz - tickHz) < 0.01) return;
	tickHz = hz;
	if (hz >= 20) {
		linkStart(hz);
		if (dispLink) {
			[tickTimer invalidate];
			tickTimer = nil;
			return;
		}
	}
	linkStop();
	[tickTimer invalidate];
	tickTimer = [NSTimer timerWithTimeInterval:1.0 / hz target:delegate selector:@selector(tick:) userInfo:nil repeats:YES];
	tickTimer.tolerance = 0.1 / hz; // lets macOS group wake-ups
	[[NSRunLoop mainRunLoop] addTimer:tickTimer forMode:NSRunLoopCommonModes];
}

void os_screens_changed(void) { if (tickHz >= 20 && dispLink) { double h = tickHz; tickHz = 0; os_set_rate(h); } }

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
		for (NSRunningApplication *a in others) if (kill(a.processIdentifier, 0) == 0) [left addObject:a];
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
		[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidChangeScreenParametersNotification object:nil queue:nil usingBlock:^(NSNotification *n) { os_screens_moved(); os_screens_changed(); goScreensChanged(); }];
		os_set_rate(2);
		[NSApp run];
	}
}

void os_set_tooltip(const char *s) { statusItem.button.toolTip = [NSString stringWithUTF8String:s]; }
void os_quit(void) { dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; }); }

// os_other_windows: where other apps' normal windows are (pixels, front to back). Asked on a
// background thread (the window server can take milliseconds to answer); s = screen scale.
int os_other_windows(double s, double *out, int max) {
	CFArrayRef list = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
	if (!list) return -1;
	int me = getpid(), n = 0;
	for (CFIndex i = 0; i < CFArrayGetCount(list) && n < max; i++) {
		NSDictionary *w = (__bridge NSDictionary *)CFArrayGetValueAtIndex(list, i);
		if ([w[(id)kCGWindowOwnerPID] intValue] == me) continue;
		if ([w[(id)kCGWindowLayer] intValue] != 0) continue; // menu bar, Dock, overlays
		CGRect r;
		if (!CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)w[(id)kCGWindowBounds], &r)) continue;
		out[4 * n] = r.origin.x * s;
		out[4 * n + 1] = r.origin.y * s;
		out[4 * n + 2] = r.size.width * s;
		out[4 * n + 3] = r.size.height * s;
		n++;
	}
	CFRelease(list);
	return n;
}

double os_double_click_time(void) { return NSEvent.doubleClickInterval; }

void os_show_menu(void) { [menu popUpMenuPositioningItem:nil atLocation:NSEvent.mouseLocation inView:nil]; }

// os_desktop_widgets: where the Mac's desktop widgets are (pixels). They are windows of the
// Notification Center (WidgetKit) under the normal windows; full-screen ones are not widgets.
int os_desktop_widgets(double s, double scrW, double scrH, double *out, int max) {
	CFArrayRef list = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly, kCGNullWindowID);
	if (!list) return 0;
	int n = 0;
	for (CFIndex i = 0; i < CFArrayGetCount(list) && n < max; i++) {
		NSDictionary *w = (__bridge NSDictionary *)CFArrayGetValueAtIndex(list, i);
		NSString *owner = w[(id)kCGWindowOwnerName];
		if (![owner isEqualToString:@"Notification Center"] && ![owner isEqualToString:@"NotificationCenter"] && ![owner hasPrefix:@"WidgetKit"]) continue;
		if ([w[(id)kCGWindowLayer] intValue] >= 0) continue; // banners and the side panel are above the windows
		CGRect r;
		if (!CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)w[(id)kCGWindowBounds], &r)) continue;
		if (r.size.width < 40 || r.size.height < 40 || r.size.width * r.size.height > 0.4 * scrW * scrH) continue;
		out[4 * n] = r.origin.x * s;
		out[4 * n + 1] = r.origin.y * s;
		out[4 * n + 2] = r.size.width * s;
		out[4 * n + 3] = r.size.height * s;
		n++;
	}
	CFRelease(list);
	return n;
}

// ow_motion: scale every picture of the widget by (sx, sy) around (cx, cy) pixels and show the soft
// layer at opacity glow — the beat, moved by the GPU
void ow_motion(void *wp, double sx, double sy, double cx, double cy, double glow) {
	OrbitWidgetView *v = (OrbitWidgetView *)((__bridge NSWindow *)wp).contentView;
	double s = os_scale();
	CALayer *root = v.layer;
	if (!root.actions) root.actions = @{@"sublayerTransform": NSNull.null};
	double px = cx / s, py = root.bounds.size.height - cy / s; // (y up)
	CATransform3D t = CATransform3DMakeTranslation(px, py, 0);
	t = CATransform3DScale(t, sx, sy, 1);
	t = CATransform3DTranslate(t, -px, -py, 0);
	root.sublayerTransform = t;
	if (v->layers[LSoft]) v->layers[LSoft].opacity = glow;
	if (v->layers[LGlowS]) v->layers[LGlowS].opacity = glow;
}

// ow_rings: n circles of light around (cx, cy) pixels — radius r, width w, opacity a — in colour c:
// the record's groove light, drawn by the GPU (n = 0: none)
void ow_rings(void *wp, int n, const double *r, const double *w, const double *a, uint32_t c, double cx, double cy) {
	OrbitWidgetView *v = (OrbitWidgetView *)((__bridge NSWindow *)wp).contentView;
	if (n <= 0) { if (v->layers[LRings]) v->layers[LRings].hidden = YES; return; }
	double s = os_scale();
	CALayer *box = [v part:LRings];
	box.hidden = NO;
	if (!CGRectEqualToRect(box.frame, v.layer.bounds)) box.frame = v.layer.bounds;
	NSArray *subs = box.sublayers;
	while ((int)subs.count < n) {
		CAShapeLayer *sl = [CAShapeLayer layer];
		NSNull *no = NSNull.null;
		sl.actions = @{@"path": no, @"lineWidth": no, @"opacity": no, @"strokeColor": no, @"hidden": no, @"position": no, @"bounds": no};
		sl.fillColor = nil;
		[box addSublayer:sl];
		subs = box.sublayers;
	}
	CGColorRef col = CGColorCreateSRGB(((c >> 16) & 255) / 255.0, ((c >> 8) & 255) / 255.0, (c & 255) / 255.0, 1);
	double px = cx / s, py = box.bounds.size.height - cy / s;
	for (int i = 0; i < (int)subs.count; i++) {
		CAShapeLayer *sl = subs[i];
		if (i >= n || a[i] <= 0.004) { sl.hidden = YES; continue; }
		sl.hidden = NO;
		CGRect cr = CGRectMake(px - r[i] / s, py - r[i] / s, 2 * r[i] / s, 2 * r[i] / s);
		CGPathRef old = sl.path;
		if (!old || !CGRectEqualToRect(CGPathGetBoundingBox(old), cr)) {
			CGPathRef p = CGPathCreateWithEllipseInRect(cr, NULL);
			sl.path = p;
			CGPathRelease(p);
		}
		sl.lineWidth = w[i] / s;
		sl.opacity = a[i];
		sl.strokeColor = col;
	}
	CGColorRelease(col);
}

void os_cgimage_release(void *img) { if (img) CGImageRelease((CGImageRef)img); }

// ow_sprites: n pictures (the lyrics' words) placed, turned, scaled and faded by the GPU — each
// with its glow picture right under it (all the glows together in one layer under all the words)
void ow_sprites(void *wp, int n, const OSprite *sp) {
	OrbitWidgetView *v = (OrbitWidgetView *)((__bridge NSWindow *)wp).contentView;
	if (n <= 0 && !v->layers[LTextS]) return;
	double s = os_scale();
	CALayer *boxes[2] = {[v part:LGlowS], [v part:LTextS]};
	CGRect b = v.layer.bounds;
	double H = b.size.height;
	for (int k = 0; k < 2; k++) {
		CALayer *box = boxes[k];
		box.hidden = n <= 0;
		if (!CGRectEqualToRect(box.frame, b)) box.frame = b;
		while ((int)box.sublayers.count < n) [box addSublayer:newLayer()];
		NSArray *subs = box.sublayers;
		for (int i = 0; i < (int)subs.count; i++) {
			CALayer *l = subs[i];
			void *img = i < n ? (k ? sp[i].img : sp[i].glow) : NULL;
			if (!img) { if (!l.hidden) l.hidden = YES; continue; }
			const OSprite *d = &sp[i];
			l.hidden = NO;
			if (l.contents != (__bridge id)img) l.contents = (__bridge id)img;
			double w = k ? d->iw : d->gw, h = k ? d->ih : d->gh, ax = k ? d->iax : d->gax, ay = k ? d->iay : d->gay;
			l.bounds = CGRectMake(0, 0, w / s, h / s);
			l.anchorPoint = CGPointMake(ax, 1 - ay); // (the turning point, in the picture; y up)
			l.position = CGPointMake(d->x / s, H - d->y / s);
			CATransform3D t = CATransform3DMakeRotation(-d->rot * M_PI / 180, 0, 0, 1);
			l.transform = CATransform3DScale(t, d->sx, d->sy, 1);
			l.opacity = d->alpha;
		}
	}
}

long os_present_bytes(void) { return livePresentBytes; }
