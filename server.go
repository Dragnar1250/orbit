package main

// Settings window backend: a tiny HTTP server on 127.0.0.1 (random port + random token).
// The window itself is settings.html, opened in Edge's app mode (looks like a normal window).

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed settings.html
var settingsHTML []byte

// Hooks are the platform-specific actions (set by the Windows code, faked in tests).
type Hooks struct {
	Fonts           func() []string
	StartupEnabled  func() bool
	SetStartup      func(bool)
	OpenFolder      func()
	Quit            func()
	SetEditMode     func(bool)
	EditMode        func() bool
	SettingsChanged func(key string)
	ResetPositions  func(which string)
	// WidgetInfo: centre (% of the work area) and size scale of each widget
	WidgetInfo func() map[string]WidgetInfo
	// SetWidget moves a widget's centre to x%,y% of the work area and sets its scale
	SetWidget func(name string, x, y, scale float64)
	// SetWidgetFlag turns "onTop" or "locked" on/off for a widget
	SetWidgetFlag func(name, flag string, on bool)
	// Flash outlines a widget for a moment ("Show me where it is")
	Flash func(name string)
	// SetOpacity: 0.15..1 for one widget
	SetOpacity func(name string, v float64)
	// FormatDate: the date in the user's own language (Windows), long or short
	FormatDate func(t time.Time, long bool) string
	// Fullscreen opens (true) or closes (false) the full-screen view
	Fullscreen func(open bool)
	// OpenSettings: the settings in Orbit's own window (the Mac)
	OpenSettings func()
	// UpdateInstall: put the downloaded update in place and restart; UpdateReady: one is ready
	UpdateInstall func()
	UpdateReady   func()
}

type WidgetInfo struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Scale   float64 `json:"scale"`
	Visible bool    `json:"visible"`
	OnTop   bool    `json:"onTop"`
	Locked  bool    `json:"locked"`
	Opacity float64 `json:"opacity"`
}

var hooks = Hooks{
	Fonts:           func() []string { return legacyFonts },
	StartupEnabled:  func() bool { return false },
	SetStartup:      func(bool) {},
	OpenFolder:      func() {},
	Quit:            func() {},
	SetEditMode:     func(bool) {},
	EditMode:        func() bool { return false },
	SettingsChanged: func(string) {},
	ResetPositions:  func(string) {},
	WidgetInfo:      func() map[string]WidgetInfo { return map[string]WidgetInfo{} },
	SetWidget:       func(string, float64, float64, float64) {},
	SetWidgetFlag:   func(string, string, bool) {},
	Flash:           func(string) {},
	SetOpacity:      func(string, float64) {},
	FormatDate:      englishDate,
	Fullscreen:      func(bool) {},
}

// uiQueue carries work to the UI thread (all drawing state lives there).
var uiQueue = make(chan func(), 512)

func runOnUI(f func()) {
	select {
	case uiQueue <- f:
	default:
		logf("ui queue full, dropping a settings change")
	}
}

func drainUI() {
	for {
		select {
		case f := <-uiQueue:
			f()
		default:
			return
		}
	}
}

func withCfg(f func()) {
	if threadID != nil && cfgOwner.Load() != 0 && cfgOwner.Load() == threadID() {
		f() // the UI thread already holds it (Cinema drawing with its own settings): no deadlock
		return
	}
	cfgMu.Lock()
	defer cfgMu.Unlock()
	f()
}

// The thread that holds cfgMu while drawing with borrowed settings (Cinema's lyrics and record),
// so that anything they call that asks for the settings doesn't wait for itself forever.
var (
	cfgOwner atomic.Uint64
	threadID func() uint64 // set on Windows
)

func lockCfgUI() {
	cfgMu.Lock()
	if threadID != nil {
		cfgOwner.Store(threadID())
	}
}

func unlockCfgUI() {
	cfgOwner.Store(0)
	cfgMu.Unlock()
}

type SettingsServer struct {
	port  int
	token string
	ln    net.Listener
}

var settingsSrv *SettingsServer

func (s *SettingsServer) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", s.port, s.token)
}

const preferredPort = 47330

// persistentToken is created once and kept in the app folder, so a settings window that was
// left open keeps working after Orbit Lyrics restarts (1.1/1.2 made a new one every start).
func persistentToken() string {
	p := filepath.Join(appDir(), "settings.key")
	if b, err := os.ReadFile(p); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) == 32 {
			if _, err := hex.DecodeString(t); err == nil {
				return t
			}
		}
	}
	tok := make([]byte, 16)
	_, _ = rand.Read(tok)
	t := hex.EncodeToString(tok)
	_ = os.WriteFile(p, []byte(t), 0o600)
	return t
}

func startSettingsServer(addr string) (*SettingsServer, error) {
	var ln net.Listener
	var err error
	if addr == "" {
		// same address every time when possible (bookmarks / open windows keep working)
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
		if err != nil {
			ln, err = net.Listen("tcp", "127.0.0.1:0")
		}
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	s := &SettingsServer{port: ln.Addr().(*net.TCPAddr).Port, token: persistentToken(), ln: ln}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logf("settings server panic: %v", r)
			}
		}()
		_ = srv.Serve(ln)
	}()
	return s, nil
}

type stateResp struct {
	Config   json.RawMessage       `json:"config"`
	Fonts    []string              `json:"fonts"`
	Skies    []constellation       `json:"skies"` // the real constellations (Constellation style previews)
	Spotify  map[string]any        `json:"spotify"`
	Update   map[string]any        `json:"update"`
	Now      map[string]any        `json:"now"`
	EditMode bool                  `json:"editMode"`
	Startup  bool                  `json:"startup"`
	Styles   []string              `json:"styles"`
	Effects  []string              `json:"effects"`
	Themes   []map[string]string   `json:"themes"`
	Corners  []string              `json:"corners"`
	Discs    []string              `json:"discs"`
	DiscCols []string              `json:"discColors"`
	Vis      []string              `json:"vis"`
	Palettes []map[string]string   `json:"palettes"`
	Cards    []string              `json:"cards"`
	Motions  []string              `json:"motions"`
	Cases    []string              `json:"cases"`
	Widgets  map[string]WidgetInfo `json:"widgets"`
	Version  string                `json:"version"`
	All      []string              `json:"all"`
	Lists    map[string][]string   `json:"lists"`
	Weather  Weather               `json:"weather"`
	Now2     string                `json:"dateLong"` // today in the user's language, as the clock shows it
	Now3     string                `json:"dateShort"`
}

func fmtDate(t time.Time, long bool) string {
	if hooks.FormatDate == nil {
		return englishDate(t, long)
	}
	return hooks.FormatDate(t, long)
}

func hexColor(c uint32) string {
	if c == 0 {
		return ""
	}
	return fmt.Sprintf("#%06X", c&0xFFFFFF)
}

func (s *SettingsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// protect against DNS-rebinding and other websites poking at localhost
	if r.Host != fmt.Sprintf("127.0.0.1:%d", s.port) && r.Host != fmt.Sprintf("localhost:%d", s.port) {
		http.Error(w, "forbidden", 403)
		return
	}
	if r.URL.Path == "/spotify/callback" && r.Method == "GET" { // Spotify's return: checked by its state, not the token
		serveSpotify(w, r, s.port)
		return
	}
	tok := r.URL.Query().Get("t")
	if tok == "" {
		tok = r.Header.Get("X-Orbit-Token")
	}
	if tok != s.token {
		http.Error(w, "forbidden", 403)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.URL.Path == "/" && r.Method == "GET":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline'; img-src 'self' data:")
		w.Write(settingsHTML)
	case r.URL.Path == "/spotify/login" && r.Method == "GET":
		serveSpotify(w, r, s.port)
	case r.URL.Path == "/api/state" && r.Method == "GET":
		s.state(w)
	case r.URL.Path == "/api/i18n" && r.Method == "GET":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprintf(w, `{"lang":%q,"platform":%q,"dict":`, langCode(), runtime.GOOS)
		w.Write(langJSON())
		w.Write([]byte("}"))
	case r.URL.Path == "/api/set" && r.Method == "POST":
		s.set(w, r)
	case r.URL.Path == "/api/geocode" && r.Method == "GET":
		q := r.URL.Query().Get("q")
		if len(q) > 100 {
			http.Error(w, "too long", 400)
			return
		}
		places, err := geocode(q)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			logf("geocode %q: %v", q, err)
			w.WriteHeader(502)
			json.NewEncoder(w).Encode(map[string]string{"error": T("Can't reach the place search right now")})
			return
		}
		out := make([]map[string]any, len(places))
		for i, p := range places {
			out[i] = map[string]any{"label": p.Label(), "name": p.Name, "admin": p.Admin, "country": p.Country, "lat": p.Lat, "lon": p.Lon}
		}
		json.NewEncoder(w).Encode(out)
	case r.URL.Path == "/api/cover" && r.Method == "GET":
		snap := media.Snapshot()
		if snap.Track == nil || len(snap.Track.Cover) == 0 {
			http.NotFound(w, r)
			return
		}
		ct := http.DetectContentType(snap.Track.Cover)
		if !strings.HasPrefix(ct, "image/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Write(snap.Track.Cover)
	case r.URL.Path == "/api/action" && r.Method == "POST":
		s.action(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *SettingsServer) state(w http.ResponseWriter) {
	var cb []byte
	var spConnected bool
	withCfg(func() {
		c := *cfg
		spConnected = c.SpotifyRefresh != "" && c.SpotifyClientID != ""
		c.SpotifyRefresh = "" // the sign-in never goes to the page
		cb, _ = json.Marshal(c)
	})
	spotify.Lock()
	spErr := spotify.lastErr
	spotify.Unlock()
	spControl := spotify.canControl()
	snap := media.Snapshot()
	now := map[string]any{"title": "", "artist": "", "playing": false}
	if snap.Track != nil {
		now = map[string]any{"title": snap.Track.Title, "artist": snap.Track.Artist, "playing": snap.Playing,
			"exact": snap.Exact, "source": snap.Source, "pos": snap.Pos, "duration": snap.Track.Duration,
			"cover": len(snap.Track.Cover) > 0, "coverVer": fmt.Sprintf("%s|%d", snap.Track.Key, snap.Track.CoverVer)}
	}
	th := make([]map[string]string, len(themes))
	for i, t := range themes {
		th[i] = map[string]string{"name": t.Name, "text": hexColor(t.Text), "accent": hexColor(t.Accent)}
	}
	pal := make([]map[string]string, len(visPalettes))
	for i, p := range visPalettes {
		pal[i] = map[string]string{"name": p.Name, "a": hexColor(p.A), "b": hexColor(p.B)}
	}
	resp := stateResp{Vis: visNames, Palettes: pal, Cards: visCardNames, Motions: motionNames, Cases: caseNames, Config: cb, Fonts: hooks.Fonts(), Now: now, EditMode: hooks.EditMode(), Startup: hooks.StartupEnabled(),
		Styles: styleNames, Effects: effectNames, Themes: th, Corners: cornerNames, Discs: discNames, DiscCols: discColorNames, Version: appVersion, Widgets: hooks.WidgetInfo(),
		All: allWidgets, Weather: weatherSnapshot(), Now2: fmtDate(time.Now(), true), Now3: fmtDate(time.Now(), false),
		Skies:   constellations,
		Spotify: map[string]any{"connected": spConnected, "control": spControl, "error": spErr, "redirect": spotifyRedirect(s.port)},
		Update:  updater.status(),
		Lists: map[string][]string{"clockStyles": clockStyleNames, "fontGroups": fontGroupNames, "clockColors": clockColorNames, "aligns": alignNames,
			"weatherStyles": weatherStyleNames, "glowStyles": glowStyleNames, "glowWhere": glowWhereNames, "glowModes": glowModeNames,
			"glowColors": glowColorNames, "glowScreens": glowScreenNames, "weatherKinds": weatherKindNames, "starShapes": starShapeNames,
			"vinylLayouts": vinylLayoutNames, "ambColors": ambColorNames, "ambEffects": ambEffectNames,
			"fsScreens": fsScreenNames, "stages": stageNames(), "uiThemes": uiThemeNames, "fsBgs": fsBgNames, "fsCovers": fsCoverNames, "fsArms": fsArmNames, "langs": langNames}}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *SettingsServer) set(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.Key == "" {
		http.Error(w, "bad request", 400)
		return
	}
	// validate on a copy first so the window gets an error right away
	var err error
	withCfg(func() {
		tmp := *cfg
		err = tmp.setField(req.Key, req.Value)
	})
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	key, val := req.Key, req.Value
	logf("settings: %s = %s", key, string(val))
	runOnUI(func() {
		withCfg(func() { _ = cfg.setField(key, val) })
		hooks.SettingsChanged(key)
	})
	w.Write([]byte(`{"ok":true}`))
}

func (s *SettingsServer) action(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	logf("settings action: %s %s", req.Name, string(req.Value))
	var b bool
	var str string
	_ = json.Unmarshal(req.Value, &b)
	_ = json.Unmarshal(req.Value, &str)
	switch req.Name {
	case "updateCheck":
		updater.check(true, func() {
			if hooks.UpdateReady != nil {
				hooks.UpdateReady()
			}
		})
	case "updateInstall":
		if hooks.UpdateInstall != nil {
			runOnUI(hooks.UpdateInstall)
		}
	case "spotifyDisconnect":
		withCfg(func() { cfg.SpotifyRefresh, cfg.SpotifyScopes = "", "" })
		spotify.Lock()
		spotify.access, spotify.queue, spotify.queueAt, spotify.lastErr = "", nil, time.Time{}, ""
		spotify.Unlock()
		saveConfigSoon()
	case "editMode":
		runOnUI(func() { hooks.SetEditMode(b) })
	case "resetPositions":
		which := strings.TrimSpace(str)
		if which == "" {
			which = "all"
		}
		runOnUI(func() { hooks.ResetPositions(which) })
	case "widget":
		var wv struct {
			Name  string  `json:"name"`
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Scale float64 `json:"scale"`
		}
		if json.Unmarshal(req.Value, &wv) != nil {
			http.Error(w, "bad widget value", 400)
			return
		}
		if !validWidget(wv.Name) {
			http.Error(w, "unknown widget", 400)
			return
		}
		x, y, sc := clampF(wv.X, -20, 120), clampF(wv.Y, -20, 120), clampF(wv.Scale, 0.3, 3)
		name := wv.Name
		runOnUI(func() { hooks.SetWidget(name, x, y, sc) })
	case "widgetFlag":
		var wf struct {
			Name string `json:"name"`
			Flag string `json:"flag"`
			On   bool   `json:"on"`
		}
		if json.Unmarshal(req.Value, &wf) != nil || (wf.Flag != "onTop" && wf.Flag != "locked") ||
			!validWidget(wf.Name) {
			http.Error(w, "bad widgetFlag value", 400)
			return
		}
		runOnUI(func() { hooks.SetWidgetFlag(wf.Name, wf.Flag, wf.On) })
	case "opacity":
		var ov struct {
			Name string  `json:"name"`
			V    float64 `json:"v"`
		}
		if json.Unmarshal(req.Value, &ov) != nil || !validWidget(ov.Name) {
			http.Error(w, "bad opacity value", 400)
			return
		}
		v := clampF(ov.V, 0.15, 1)
		runOnUI(func() { hooks.SetOpacity(ov.Name, v) })
	case "weatherPlace":
		var pl struct {
			Label string  `json:"label"`
			Lat   float64 `json:"lat"`
			Lon   float64 `json:"lon"`
		}
		if json.Unmarshal(req.Value, &pl) != nil || strings.TrimSpace(pl.Label) == "" || pl.Lat < -90 || pl.Lat > 90 || pl.Lon < -180 || pl.Lon > 180 {
			http.Error(w, "bad place", 400)
			return
		}
		runOnUI(func() {
			withCfg(func() {
				cfg.WeatherCity, cfg.WeatherLat, cfg.WeatherLon = strings.TrimSpace(pl.Label), pl.Lat, pl.Lon
				cfg.validateFields()
			})
			hooks.SettingsChanged("WeatherCity")
			kickWeather()
		})
	case "weatherRefresh":
		weather.Lock()
		weather.key = "" // force a fresh download
		weather.Unlock()
		kickWeather()
	case "fullscreen":
		runOnUI(func() { hooks.Fullscreen(b) })
	case "flash":
		name := strings.TrimSpace(str)
		if !validWidget(name) {
			http.Error(w, "unknown widget", 400)
			return
		}
		runOnUI(func() { hooks.Flash(name) })
	case "startup":
		runOnUI(func() { hooks.SetStartup(b) })
	case "openFolder":
		runOnUI(hooks.OpenFolder)
	case "openSettings":
		if hooks.OpenSettings != nil {
			hooks.OpenSettings()
		}
	case "quit":
		runOnUI(hooks.Quit)
	default:
		http.Error(w, "unknown action", 400)
		return
	}
	w.Write([]byte(`{"ok":true}`))
}
