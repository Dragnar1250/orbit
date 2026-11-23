// Orbit for macOS: the Cocoa side (menu bar, the lyrics window). Go calls the orbit_* functions;
// the timer and the menu call back into Go (goTick, goMenu).
#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"


@interface OrbitLyricView : NSView
@property (strong) NSString *line;
@property (strong) NSString *next;
@property CGFloat alpha;
@property CGFloat size;
@end

@implementation OrbitLyricView
- (BOOL)isFlipped { return YES; }
- (void)drawRect:(NSRect)dirty {
	if (self.line.length == 0) return;
	NSShadow *sh = [NSShadow new];
	sh.shadowBlurRadius = 14;
	sh.shadowOffset = NSMakeSize(0, -1);
	sh.shadowColor = [NSColor colorWithWhite:0 alpha:0.85];
	NSMutableParagraphStyle *ps = [NSMutableParagraphStyle new];
	ps.alignment = NSTextAlignmentCenter;
	ps.lineBreakMode = NSLineBreakByWordWrapping;
	NSDictionary *a = @{NSFontAttributeName: [NSFont systemFontOfSize:self.size weight:NSFontWeightBold],
		NSForegroundColorAttributeName: NSColor.whiteColor, // fading is the layer's opacity (done by the GPU)
		NSShadowAttributeName: sh, NSParagraphStyleAttributeName: ps};
	NSRect b = self.bounds;
	CGFloat w = b.size.width - 40;
	NSRect r1 = [self.line boundingRectWithSize:NSMakeSize(w, 10000) options:NSStringDrawingUsesLineFragmentOrigin attributes:a];
	CGFloat h2 = 0;
	NSDictionary *a2 = nil;
	if (self.next.length) {
		a2 = @{NSFontAttributeName: [NSFont systemFontOfSize:self.size * 0.5 weight:NSFontWeightMedium],
			NSForegroundColorAttributeName: [NSColor colorWithWhite:1 alpha:0.55],
			NSShadowAttributeName: sh, NSParagraphStyleAttributeName: ps};
		h2 = [self.next boundingRectWithSize:NSMakeSize(w, 10000) options:NSStringDrawingUsesLineFragmentOrigin attributes:a2].size.height + 8;
	}
	CGFloat y = (b.size.height - r1.size.height - h2) / 2;
	[self.line drawWithRect:NSMakeRect(20, y, w, r1.size.height) options:NSStringDrawingUsesLineFragmentOrigin attributes:a];
	if (a2) [self.next drawWithRect:NSMakeRect(20, y + r1.size.height + 8, w, h2) options:NSStringDrawingUsesLineFragmentOrigin attributes:a2];
}
@end

@interface OrbitDelegate : NSObject <NSApplicationDelegate>
@end
@implementation OrbitDelegate
- (void)menu:(NSMenuItem *)m { goMenu((int)m.tag); }
- (void)tick:(NSTimer *)t { goTick(); }
@end

static NSWindow *lyricWin;
static OrbitLyricView *lyricView;
static NSStatusItem *statusItem;
static OrbitDelegate *delegate;
static NSMenuItem *nowItem, *lyricsItem;
static NSTimer *tickTimer;
static double tickHz;

// orbit_set_rate: how often Go is asked for the next frame (2 a second when idle, more while
// a song plays) — the fades themselves run on the GPU without any ticks.
void orbit_set_rate(double hz) {
	if (hz == tickHz) return;
	tickHz = hz;
	[tickTimer invalidate];
	tickTimer = [NSTimer timerWithTimeInterval:1.0 / hz target:delegate selector:@selector(tick:) userInfo:nil repeats:YES];
	tickTimer.tolerance = 0.2 / hz; // lets macOS group wake-ups (saves power)
	[[NSRunLoop mainRunLoop] addTimer:tickTimer forMode:NSRunLoopCommonModes];
}

static NSMenuItem *addItem(NSMenu *m, NSString *title, int tag) {
	NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title action:@selector(menu:) keyEquivalent:@""];
	it.target = delegate;
	it.tag = tag;
	[m addItem:it];
	return it;
}

static void placeLyrics(void) {
	NSRect sc = [NSScreen mainScreen].visibleFrame;
	NSRect fr = NSMakeRect(sc.origin.x + sc.size.width * 0.08, sc.origin.y + sc.size.height * 0.55, sc.size.width * 0.84, sc.size.height * 0.3);
	[lyricWin setFrame:fr display:YES];
}

void orbit_run(const char *iconPath) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]; // menu bar only, no Dock icon
		delegate = [OrbitDelegate new];
		NSApp.delegate = delegate;

		lyricWin = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 800, 240) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO];
		lyricWin.opaque = NO;
		lyricWin.backgroundColor = NSColor.clearColor;
		lyricWin.hasShadow = NO;
		lyricWin.ignoresMouseEvents = YES;
		lyricWin.level = CGWindowLevelForKey(kCGDesktopIconWindowLevelKey) + 1; // on the desktop, under every window
		lyricWin.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary | NSWindowCollectionBehaviorIgnoresCycle;
		lyricView = [[OrbitLyricView alloc] initWithFrame:NSMakeRect(0, 0, 800, 240)];
		lyricView.wantsLayer = YES; // composited on the GPU
		lyricView.layerContentsRedrawPolicy = NSViewLayerContentsRedrawOnSetNeedsDisplay; // redrawn only when the text changes
		lyricView.size = 44;
		lyricView.alphaValue = 0;
		lyricWin.contentView = lyricView;
		placeLyrics();
		[lyricWin orderFront:nil];
		[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidChangeScreenParametersNotification object:nil queue:nil usingBlock:^(NSNotification *n) { placeLyrics(); }];

		statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
		NSImage *img = iconPath ? [[NSImage alloc] initWithContentsOfFile:[NSString stringWithUTF8String:iconPath]] : nil;
		if (img) {
			img.size = NSMakeSize(18, 18);
			img.template = YES; // tinted for light and dark menu bars
			statusItem.button.image = img;
		} else {
			statusItem.button.title = @"◉";
		}
		statusItem.button.toolTip = @"Orbit";
		NSMenu *menu = [NSMenu new];
		nowItem = addItem(menu, @"Nothing playing", 0);
		nowItem.enabled = NO;
		[menu addItem:[NSMenuItem separatorItem]];
		addItem(menu, @"Settings…", 1);
		lyricsItem = addItem(menu, @"Lyrics on the desktop", 2);
		[menu addItem:[NSMenuItem separatorItem]];
		addItem(menu, @"Play / pause", 3);
		addItem(menu, @"Next song", 4);
		addItem(menu, @"Previous song", 5);
		[menu addItem:[NSMenuItem separatorItem]];
		addItem(menu, @"Quit Orbit", 9);
		menu.autoenablesItems = NO;
		statusItem.menu = menu;

		orbit_set_rate(2);
		// Spotify and Music announce every change: Orbit asks them only then (and now and then)
		for (NSString *name in @[@"com.spotify.client.PlaybackStateChanged", @"com.apple.Music.playerInfo"]) {
			[[NSDistributedNotificationCenter defaultCenter] addObserverForName:name object:nil queue:nil usingBlock:^(NSNotification *n) { goMediaPing(); }];
		}
		[NSApp run];
	}
}

// called on the main thread (from goTick)
// Go calls this only when something changed. A new line fades in on the GPU; a pause dims it.
void orbit_set_lyric(const char *line, const char *next, double alpha, double size, int show, int newLine) {
	NSString *l = [NSString stringWithUTF8String:line];
	NSString *n = [NSString stringWithUTF8String:next];
	BOOL text = ![l isEqualToString:lyricView.line ?: @""] || ![n isEqualToString:lyricView.next ?: @""] || size != lyricView.size;
	lyricView.line = l;
	lyricView.next = n;
	lyricView.size = size;
	lyricsItem.state = show ? NSControlStateValueOn : NSControlStateValueOff;
	if (show && !lyricWin.isVisible) [lyricWin orderFront:nil];
	if (!show && lyricWin.isVisible) [lyricWin orderOut:nil];
	if (text) lyricView.needsDisplay = YES;
	if (newLine) lyricView.alphaValue = 0;
	[NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
		ctx.duration = newLine ? 0.25 : 0.4;
		lyricView.animator.alphaValue = alpha;
	}];
}

void orbit_set_now(const char *now) {
	nowItem.title = [NSString stringWithUTF8String:now];
	statusItem.button.toolTip = nowItem.title;
}

void orbit_quit(void) {
	dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
}
