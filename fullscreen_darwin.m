// Full screen on the Mac: a borderless window over the whole screen (menu bar and Dock too),
// its keys and mouse turned into the same calls the Windows window messages make; and the music
// video in a WebKit window right under it.
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <WebKit/WebKit.h>
#include "_cgo_export.h"
#include "gdip_darwin.h"
#include "app_darwin.h"
#include "fullscreen_darwin.h"

@interface OrbitFSWindow : NSWindow
@end
@implementation OrbitFSWindow
- (BOOL)canBecomeKeyWindow { return YES; }
- (BOOL)canBecomeMainWindow { return YES; }
@end

// Windows virtual-key codes for the Mac's keys (full screen's key handling is shared)
static int vkOf(NSEvent *e) {
	switch (e.keyCode) {
	case 53: return 0x1B; // esc
	case 49: return 0x20; // space
	case 123: return 0x25;
	case 124: return 0x27;
	case 126: return 0x26;
	case 125: return 0x28;
	case 36: case 76: return 0x0D;
	case 51: return 0x08;
	case 103: return 0x7A; // F11
	case 122: return 0x70; // F1
	case 43: return 0xBC; // ,
	case 47: return 0xBE; // .
	case 44: return 0xBF; // /
	}
	NSString *c = e.charactersIgnoringModifiers.uppercaseString;
	if (c.length == 1) {
		unichar u = [c characterAtIndex:0];
		if ((u >= 'A' && u <= 'Z') || (u >= '0' && u <= '9')) return u;
	}
	return 0;
}

@interface OrbitFSView : NSView
@end
@implementation OrbitFSView
- (BOOL)acceptsFirstResponder { return YES; }
- (BOOL)acceptsFirstMouse:(NSEvent *)e { return YES; }
- (BOOL)isFlipped { return YES; }
- (void)updateTrackingAreas {
	for (NSTrackingArea *a in self.trackingAreas) [self removeTrackingArea:a];
	[self addTrackingArea:[[NSTrackingArea alloc] initWithRect:self.bounds options:NSTrackingMouseMoved | NSTrackingActiveAlways | NSTrackingInVisibleRect owner:self userInfo:nil]];
	[super updateTrackingAreas];
}
- (NSPoint)px:(NSEvent *)e {
	NSPoint p = [self convertPoint:e.locationInWindow fromView:nil];
	double s = self.window.backingScaleFactor ?: 1;
	return NSMakePoint(p.x * s, p.y * s);
}
- (void)keyDown:(NSEvent *)e {
	if ((e.modifierFlags & NSEventModifierFlagCommand) && ([e.charactersIgnoringModifiers isEqualToString:@"q"] || [e.charactersIgnoringModifiers isEqualToString:@"w"])) {
		goFSEvent(9, 0, 0, 0); // ⌘Q / ⌘W: leave full screen (like Alt+F4)
		return;
	}
	int vk = vkOf(e);
	if (vk) goFSEvent(1, 0, 0, vk);
	NSString *ch = e.characters;
	for (NSUInteger i = 0; i < ch.length; i++) {
		unichar u = [ch characterAtIndex:i];
		if (u == 0x7F) u = 8; // delete → backspace
		if (u == 3) u = 13;   // enter (keypad)
		if (u >= 0xF700 && u <= 0xF8FF) continue; // arrows, function keys: not text
		goFSEvent(2, 0, 0, u);
	}
}
- (void)mouseDown:(NSEvent *)e { NSPoint p = [self px:e]; goFSEvent(e.clickCount >= 2 ? 5 : 3, p.x, p.y, 0); }
- (void)mouseUp:(NSEvent *)e { NSPoint p = [self px:e]; goFSEvent(4, p.x, p.y, 0); }
- (void)mouseDragged:(NSEvent *)e { NSPoint p = [self px:e]; goFSEvent(8, p.x, p.y, 0); }
- (void)mouseMoved:(NSEvent *)e { NSPoint p = [self px:e]; goFSEvent(8, p.x, p.y, 0); }
- (void)rightMouseUp:(NSEvent *)e { goFSEvent(6, 0, 0, 0); }
- (void)scrollWheel:(NSEvent *)e {
	double d = e.hasPreciseScrollingDeltas ? e.scrollingDeltaY * 4 : e.scrollingDeltaY * 40;
	if (fabs(d) < 1) return;
	NSPoint p = [self px:e];
	goFSEvent(7, p.x, p.y, (int)d);
}
@end

static OrbitFSWindow *fsWin;
static NSWindow *videoWin;
static int cursorHidden;

static NSScreen *screenFor(double x, double y, double w, double h) {
	double s = os_scale();
	NSPoint c = NSMakePoint((x + w / 2) / s, NSMaxY(NSScreen.screens[0].frame) - (y + h / 2) / s);
	for (NSScreen *sc in NSScreen.screens) if (NSPointInRect(c, sc.frame)) return sc;
	return NSScreen.mainScreen;
}

void *fs_open(double x, double y, double w, double h) {
	NSScreen *sc = screenFor(x, y, w, h);
	fsWin = [[OrbitFSWindow alloc] initWithContentRect:sc.frame styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO screen:sc];
	[fsWin setFrame:sc.frame display:NO];
	fsWin.opaque = NO; // see-through where the music video shows
	fsWin.backgroundColor = NSColor.clearColor;
	fsWin.hasShadow = NO;
	fsWin.releasedWhenClosed = NO;
	fsWin.level = NSMainMenuWindowLevel + 1; // over the menu bar and the Dock
	fsWin.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
	fsWin.acceptsMouseMovedEvents = YES;
	OrbitFSView *v = [[OrbitFSView alloc] initWithFrame:NSMakeRect(0, 0, sc.frame.size.width, sc.frame.size.height)];
	v.wantsLayer = YES;
	v.layer.contentsGravity = kCAGravityResize;
	v.layerContentsRedrawPolicy = NSViewLayerContentsRedrawNever;
	fsWin.contentView = v;
	[NSApp activateIgnoringOtherApps:YES];
	[fsWin makeKeyAndOrderFront:nil];
	[fsWin makeFirstResponder:v];
	return (__bridge void *)fsWin;
}

static void *fsSurfaces;

void fs_show(OG *g) {
	if (!fsWin) return;
	NSView *v = fsWin.contentView;
	os_present_layer((__bridge void *)v.layer, &fsSurfaces, g, fsWin.backingScaleFactor);
}

void fs_close(void) {
	fs_cursor(1);
	if (fsWin) { [fsWin orderOut:nil]; fsWin.contentView.layer.contents = nil; [fsWin close]; fsWin = nil; }
	os_present_free(&fsSurfaces);
}

int fs_in_front(void) { return fsWin && fsWin.isKeyWindow && NSApp.isActive; }
void fs_refocus(void) { if (fsWin) { [NSApp activateIgnoringOtherApps:YES]; [fsWin makeKeyAndOrderFront:nil]; } }

void fs_cursor(int show) {
	if (show && cursorHidden) { [NSCursor unhide]; cursorHidden = 0; }
	if (!show && !cursorHidden) { [NSCursor hide]; cursorHidden = 1; }
}

/* ---------- the music video: Orbit's video page in WebKit, right under full screen ---------- */

void fs_video_open(const char *url) {
	if (videoWin || !fsWin) return;
	videoWin = [[NSWindow alloc] initWithContentRect:fsWin.frame styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO screen:fsWin.screen];
	[videoWin setFrame:fsWin.frame display:NO];
	videoWin.releasedWhenClosed = NO;
	videoWin.ignoresMouseEvents = YES;
	videoWin.level = NSMainMenuWindowLevel; // just under full screen
	videoWin.backgroundColor = NSColor.blackColor;
	videoWin.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
	WKWebViewConfiguration *c = [WKWebViewConfiguration new];
	c.mediaTypesRequiringUserActionForPlayback = WKAudiovisualMediaTypeNone; // plays by itself (muted)
	c.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
	WKWebView *wv = [[WKWebView alloc] initWithFrame:videoWin.contentView.bounds configuration:c];
	wv.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	videoWin.contentView = wv;
	[wv loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:[NSString stringWithUTF8String:url]]]];
	[videoWin orderFront:nil];
	[videoWin orderWindow:NSWindowBelow relativeTo:fsWin.windowNumber];
}

void fs_video_close(void) {
	if (!videoWin) return;
	WKWebView *wv = (WKWebView *)videoWin.contentView;
	if ([wv isKindOfClass:[WKWebView class]]) [wv loadHTMLString:@"" baseURL:nil];
	[videoWin orderOut:nil];
	[videoWin close];
	videoWin = nil;
}

int fs_video_open_now(void) { return videoWin != nil; }

/* ---------- a small menu at the mouse (search services) ---------- */

@interface OrbitPick : NSObject
@property int picked;
@end
@implementation OrbitPick
- (void)pick:(NSMenuItem *)m { self.picked = (int)m.tag; }
@end

int os_popup(const char **items, int n, int checked) {
	NSMenu *m = [NSMenu new];
	m.autoenablesItems = NO;
	OrbitPick *p = [OrbitPick new];
	p.picked = -1;
	for (int i = 0; i < n; i++) {
		NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:[NSString stringWithUTF8String:items[i]] action:@selector(pick:) keyEquivalent:@""];
		it.target = p;
		it.tag = i;
		it.state = i == checked ? NSControlStateValueOn : NSControlStateValueOff;
		[m addItem:it];
	}
	[m popUpMenuPositioningItem:nil atLocation:NSEvent.mouseLocation inView:nil]; // waits until closed
	return p.picked;
}

int os_open_url(const char *u) {
	NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:u]];
	return url && [[NSWorkspace sharedWorkspace] openURL:url];
}
