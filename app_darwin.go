//go:build darwin

package main

// Orbit for macOS — first version: a menu-bar icon, the settings window (the same page as on
// Windows), and synced lyrics floating on the desktop for whatever Spotify or Apple Music plays.

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void orbit_run(const char *iconPath);
void orbit_set_lyric(const char *line, const char *next, double alpha, double size, int show, int newLine);
void orbit_set_rate(double hz);
void orbit_set_now(const char *now);
void orbit_quit(void);
*/
import "C"

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
	"unsafe"
)

func init() { runtime.LockOSThread() } // Cocoa wants the main thread

var (
	macLyrVer int
	macLine   = -2
	macNow    string
	macSent   lyricSent // what Cocoa shows now: sent again only when it changes
	macRate   float64
)

type lyricSent struct {
	line, next  string
	alpha, size float64
	show        bool
}

func main() {
	logf("Orbit %s starting (macOS)", appVersion)
	// a small heap, like on Windows: collect a little more often, stay near what is in use
	debug.SetGCPercent(60)
	debug.SetMemoryLimit(96 << 20)
	if f, err := os.OpenFile(filepath.Join(appDir(), "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(f, "=== %s  Orbit %s started ===\n", time.Now().Format("2006-01-02 15:04:05"), appVersion)
		debug.SetCrashOutput(f, debug.CrashOptions{}) // a crash leaves its details here
		f.Close()
	}
	loadConfig()
	media.SetOffset(cfg.OffsetSec)
	hooks.Quit = func() { saveConfigIfDue(true); C.orbit_quit() }
	hooks.OpenFolder = func() { exec.Command("open", appDir()).Start() }
	hooks.StartupEnabled = loginItemEnabled
	hooks.SetStartup = func(on bool) { setLoginItem(on) }
	hooks.Fonts = macFonts
	hooks.WidgetInfo = func() map[string]WidgetInfo {
		return map[string]WidgetInfo{"lyrics": {X: 50, Y: 30, Scale: 1, Visible: cfg.ShowLyrics}}
	}
	if srv, err := startSettingsServer(""); err == nil {
		settingsSrv = srv
		logf("settings window on 127.0.0.1:%d", srv.port)
	} else {
		logf("settings server failed: %v", err)
	}
	go macMediaLoop()
	if _, err := os.Stat(filepath.Join(appDir(), "config.json")); err != nil {
		saveConfigSoon()
		go func() { time.Sleep(time.Second); openSettings() }()
	}
	icon := C.CString(menuIconPath())
	C.orbit_run(icon) // runs until Quit
	C.free(unsafe.Pointer(icon))
	saveConfigIfDue(true)
	logf("exit")
}

func openSettings() {
	if settingsSrv != nil {
		exec.Command("open", settingsSrv.URL()).Start()
	}
}

//export goMenu
func goMenu(tag C.int) {
	switch tag {
	case 1:
		openSettings()
	case 2:
		withCfg(func() { cfg.ShowLyrics = !cfg.ShowLyrics })
		saveConfigSoon()
	case 3:
		macControl("playpause")
	case 4:
		macControl("next track")
	case 5:
		macControl("previous track")
	case 9:
		hooks.Quit()
	}
}

// goTick: 30 times a second on the main thread — settings changes, the lyric line, the menu.
//
//export goTick
func goTick() {
	drainUI()
	saveConfigIfDue(false)
	snap := media.Snapshot()
	if snap.Version != macLyrVer {
		macLyrVer = snap.Version
		lyrics.Load(snap.Track)
	}
	now := "Nothing playing"
	if snap.Track != nil {
		now = snap.Track.Title + " — " + snap.Track.Artist
	}
	if now != macNow {
		macNow = now
		s := C.CString(now)
		C.orbit_set_now(s)
		C.free(unsafe.Pointer(s))
	}
	line, next, alpha, newLine := macLyricFrame(snap)
	want := lyricSent{line, next, alpha, macTextSize(), cfg.ShowLyrics}
	if want != macSent || newLine {
		macSent = want
		show, nl := 0, 0
		if want.show {
			show = 1
		}
		if newLine {
			nl = 1
		}
		l, n := C.CString(line), C.CString(next)
		C.orbit_set_lyric(l, n, C.double(alpha), C.double(want.size), C.int(show), C.int(nl))
		C.free(unsafe.Pointer(l))
		C.free(unsafe.Pointer(n))
	}
	// ticks only as often as needed: ~12 a second switches lines on time; 2 when idle
	rate := 2.0
	if snap.Track != nil && snap.Playing && cfg.ShowLyrics {
		rate = 12
	}
	if rate != macRate {
		macRate = rate
		C.orbit_set_rate(C.double(rate))
	}
}

//export goMediaPing
func goMediaPing() {
	select {
	case mediaPing <- struct{}{}:
	default:
	}
}

func macTextSize() float64 { return math.Max(18, math.Min(110, float64(cfg.TextSize)*1.1)) } // (main thread: reads cfg directly)

// macLyricFrame: the line to show, the next one, how visible (dimmed while paused), and whether
// the line just changed (Cocoa fades it in on the GPU).
func macLyricFrame(snap MediaSnap) (string, string, float64, bool) {
	if snap.Track == nil {
		macLine = -2
		return "", "", 0, false
	}
	key, status, ls := lyrics.Get()
	if key != snap.Track.Key || len(ls) == 0 || (status != lyrSynced && status != lyrPlain) {
		return "", "", 0, false
	}
	return lyricFrame(ls, snap.Pos+visualLead(cfg.LyricsStyle), snap.Playing, cfg.ShowPrevLine, &macLine)
}

func menuIconPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	p := filepath.Join(filepath.Dir(exe), "..", "Resources", "menubar.png") // inside Orbit.app
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func macFonts() []string {
	out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e",
		"ObjC.import('AppKit'); $.NSFontManager.sharedFontManager.availableFontFamilies.js.map(f => f.js).join('\\n')").Output()
	if err != nil {
		return []string{"Helvetica Neue", "SF Pro", "Menlo", "Avenir Next", "Georgia"}
	}
	var fs []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f = strings.TrimSpace(f); f != "" && !strings.HasPrefix(f, ".") {
			fs = append(fs, f)
		}
	}
	return fs
}

// start at login: a LaunchAgent pointing at this app
func loginItemPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.dragnar1250.orbit.plist")
}

func loginItemEnabled() bool { _, err := os.Stat(loginItemPath()); return err == nil }

func setLoginItem(on bool) {
	if !on {
		os.Remove(loginItemPath())
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.dragnar1250.orbit</string>
<key>ProgramArguments</key><array><string>%s</string></array>
<key>RunAtLoad</key><true/>
<key>ProcessType</key><string>Interactive</string>
</dict></plist>
`, xmlEscape(exe))
	os.MkdirAll(filepath.Dir(loginItemPath()), 0o755)
	os.WriteFile(loginItemPath(), []byte(plist), 0o644)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
