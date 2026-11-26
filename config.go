package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type WidgetPos struct {
	X, Y    int     // screen pixels (top-left; for lyrics: centre)
	Scale   float64 // user scale (mouse wheel in edit mode)
	Set     bool    // false = use the default position
	OnTop   bool    `json:",omitempty"` // float above other windows instead of sitting on the desktop
	Locked  bool    `json:",omitempty"` // ignore the mouse (no drag / resize / click actions)
	Opacity float64 `json:",omitempty"` // 0 = 100 %; else 0.15..1
}

type HistItem struct {
	Key, Title, Artist string
	CoverFile          string
}

// Lyrics styles (same order as Weyv)
var styleNames = []string{"Flow", "Word reel", "Cinematic", "Kinetic", "Typewriter", "Neon", "Subtitle", "Stack", "Rise", "Pop", "Karaoke", "Scatter",
	"Glass", "Bounce", "Wave", "Glitch", "Decode", "Highlighter", "Echo", "Orbit", "Slide", "Spotlight",
	"Rain", "Swing", "Split", "Focus", "Ink", "Constellation", "Bouncing", "Split-flap", "LED board"}

const (
	stFlow = iota
	stWordReel
	stCinematic
	stKinetic
	stTypewriter
	stNeon
	stSubtitle
	stStack
	stRise
	stPop
	stKaraoke
	stScatter
	stGlass
	stBounce
	stWave
	stGlitch
	stDecode
	stHighlighter
	stEcho
	stOrbit
	stSlide
	stSpotlight
	stRain
	stSwing
	stSplit
	stFocus
	stInk
	stConstellation
	stBouncing
	stFlap // 1.19: a departures board — every letter flips through others before it settles
	stLED  // 1.19: a scrolling LED dot-matrix panel
)

var effectNames = []string{"None", "Soft glow", "Strong glow", "Shadow", "Outline", "Neon", "Gradient", "Chromatic", "Long shadow", "Hollow", "Soft blur", "Glass"}

const (
	fxNone = iota
	fxSoftGlow
	fxStrongGlow
	fxShadow
	fxOutline
	fxNeon
	fxGradient
	fxChromatic
	fxLongShadow
	fxHollow
	fxSoftBlur
	fxGlass
)

type theme struct {
	Name   string
	Text   uint32
	Accent uint32 // 0 = taken from the album cover
}

var themes = []theme{
	{"Mono", 0xFFFFFFFF, 0xFFF2F2F2},
	{"Album colours", 0xFFFFFFFF, 0},
	{"Gold", 0xFFFFF8EA, 0xFFE8B04A},
	{"Ember", 0xFFFFF1E6, 0xFFFF8A3D},
	{"Ice", 0xFFEAF6FF, 0xFF7FD1FF},
	{"Neon", 0xFFFFFFFF, 0xFFFF4FD8},
	{"Mint", 0xFFEFFFF8, 0xFF2DE1C2},
	{"Black hole", 0xFFFFF4E0, 0xFFFF9A3C},
	{"Aurora", 0xFFEFFFFA, 0xFF7CF5C8},
	{"Vaporwave", 0xFFFFF0FB, 0xFFFF71CE},
	{"Ocean", 0xFFEAF8FF, 0xFF4FC3F7},
	{"Crimson", 0xFFFFECEC, 0xFFFF3B3B},
	{"Lavender", 0xFFF6F0FF, 0xFFB39DDB},
	{"Custom", 0, 0}, // must stay last
}

var cornerNames = []string{"Bottom left", "Bottom right", "Top left", "Top right"}

// 0 and 1 keep their meaning from 1.0–1.5
var discNames = []string{"Picture disc", "Black vinyl", "Coloured", "Splatter", "Marble", "Clear", "Gold", "CD (holographic)", "Neon", "Two-tone", "Glow in the dark"}

const (
	discPicture = iota
	discBlack
	discColoured
	discSplatter
	discMarble
	discClear
	discGold
	discCD       // 1.19: a spinning CD, rainbow reflections that drift and flash on the bass
	discNeon     // 1.19: black vinyl with glowing neon rings
	discTwoTone  // 1.19: half one colour, half a lighter one
	discGlowDark // 1.19: glow-in-the-dark green
)

// allWidgets: every desktop widget, in the order the settings show them.
var allWidgets = []string{"halo", "lyrics", "vinyl", "clock", "weather"}

func validWidget(name string) bool {
	for _, w := range allWidgets {
		if w == name {
			return true
		}
	}
	return false
}

var clockStyleNames = []string{"Digital", "Stacked", "Minimal", "Analog", "Words"}

const (
	ckDigital = iota
	ckStacked
	ckMinimal
	ckAnalog
	ckWords
)

var fontGroupNames = []string{"Modern", "Condensed", "Serif", "Mono", "Handwritten"}
var clockColorNames = []string{"Solid", "Gradient", "Match the visualizer"}
var alignNames = []string{"Left", "Centre", "Right"}
var weatherStyleNames = []string{"Card", "Minimal", "Forecast"}

var glowStyleNames = []string{"Soft", "Neon", "Bars", "LED strip", "Breathe", "Comets", "Aurora", "Oscilloscope", "Embers", "Prism"}

const (
	glSoft = iota
	glNeon
	glBars
	glLED
	glBreathe
	glComets
	glAurora
	glScope
	glEmbers
	glPrism
)

var glowWhereNames = []string{"All edges", "Bottom", "Sides", "Top and bottom"}
var glowModeNames = []string{"With the music", "Steady rhythm"}
var glowColorNames = []string{"Match the visualizer", "Album colours", "My own", "Rainbow"}
var glowScreenNames = []string{"Every screen", "Main screen only"}

var visNames = []string{"Halo", "Ring", "Bars", "Mirror", "Blocks", "Wave", "Particles", "Oscilloscope", "Liquid"}

const (
	visHalo = iota
	visRing
	visBars
	visMirror
	visBlocks
	visWave
	visParticles // 1.19: album-coloured particles burst out on every kick and drift back
	visScope     // 1.19: the waveform as a green phosphor trace
	visLiquid    // 1.19: a soft blob around the cover that the music shapes
)

// visualizer colour gradients (from → to). 0 = lyrics theme, 1 = album, last = custom
type palette struct {
	Name string
	A, B uint32
}

var visPalettes = []palette{
	{"Lyrics theme", 0, 0}, {"Album colours", 0, 0},
	{"Nebula", 0xFF7B5CFF, 0xFFE15BFF}, {"Ember", 0xFFFF4D3D, 0xFFFFB13D}, {"Mint", 0xFF2DE1A0, 0xFF3DB4FF},
	{"Ice", 0xFF8FD8FF, 0xFF4F7BFF}, {"Sunset", 0xFFFF5E7E, 0xFFFFC46B}, {"Mono", 0xFFFFFFFF, 0xFFBFC3CC},
	{"Custom", 0, 0},
}

var visCardNames = []string{"Off", "Compact", "Centred"}
var motionNames = []string{"Still", "Pulse", "Stretch", "Glow", "Dissolve"}
var caseNames = []string{"As written", "ALL CAPS", "all lowercase"}

const (
	moStill = iota
	moPulse
	moStretch
	moGlow
	moDissolve
)

// full screen
var fsScreenNames = []string{"Where the mouse is", "Main screen"}
var uiThemeNames = []string{"Like Windows", "Dark", "Light"}
var fsBgNames = []string{"Cover colours", "Cover", "Gradient", "Dark", "Flowing colours"}
var fsCoverNames = []string{"Cover", "Big cover", "Vinyl"}

// the full-screen tonearm's colours (0 = no arm)
var fsArmNames = []string{"No arm", "Silver", "Black", "Gold"}

const (
	cbColours  = iota // the cover blurred into soft light
	cbCover           // the cover itself across the screen, darkened
	cbGradient        // two of the cover's colours, corner to corner
	cbDark            // near black with a faint glow of the album's colour
	cbFlow            // the cover's colours drifting slowly (moving)
)

// stageNames: the Cinema lyrics stage — "Lines" (Apple-style, line by line) and every lyric style.
func stageNames() []string { return append([]string{"Lines"}, styleNames...) }

var vinylLayoutNames = []string{"Free", "Corner"}

var swapStyleNames = []string{"Lift & drop", "Sleeve"}
var ambColorNames = []string{"Album colours", "Lyrics theme", "My own", "Slow rainbow"}
var ambEffectNames = []string{"Still", "Breathe", "Beat", "Flicker", "Drift"}

var discColorNames = []string{"Match the lyrics theme", "Album colours", "My own"}

type Config struct {
	Version int

	// lyrics
	ShowLyrics   bool
	LyricsStyle  int
	LyricsPlace  int     // 0 = random spot each line, 1 = fixed
	TextSize     float64 // px at 1080p
	FontName     string
	Bold         bool
	Upper        bool    // legacy, folded into TextCase
	TextCase     int     // 0 as written, 1 ALL CAPS, 2 lowercase
	Motion       int     // reacts to the music: still / pulse / stretch / glow / dissolve
	MotionAmount float64 // 0..2
	StarShape    int     // Constellation: the symbol on every star
	Effect       int
	GlowAmount   float64 // 0..2
	Theme        int
	TextColor    string // used by the Custom theme
	AccentColor  string
	ShowPrevLine bool
	LyricsWidth  float64 // max line width, % of screen
	OffsetSec    float64

	// turntable
	ShowVinyl     bool
	DiscStyle     int
	VinylCorner   int
	SpinRPM       float64
	ShowArm       bool
	ShowList      bool
	ListCount     int
	DiscColorMode int     // 0 theme accent, 1 album colours, 2 DiscColor
	DiscColor     string  // "My own"
	GrooveLight   bool    // the music lights up the grooves (bass outside)
	DiscGlow      float64 // 0..2 glow around the record, pulses with the bass
	Shine         bool    // light reflections on the record
	ArmLift       bool    // needle lowers when music starts, lifts when it stops
	ClickPlay     bool    // single click on the record = play / pause
	VinylLayout   int     // free / corner (half the record tucked into the screen corner)
	Worn          bool    // 1.19: dust and fine scratches on the record, sparkles where the needle runs
	SwapAnim      bool    // 1.19: new song → the record is changed (animated)
	SwapStyle     int     // 1.19.1: 0 lift & drop (fade), 1 sleeve
	VinylNext     int     // 1.19.1: the song wheel shows 0 what plays next (Spotify queue), 1 what played
	Scratch       bool    // 1.19: hold the record and turn it to scrub through the song
	ScratchSec    float64 // 1.19: seconds of song for one full turn of the record
	// ambient light: a soft glow behind the record and the visualizer
	AmbVinyl     bool
	AmbHalo      bool
	AmbColorMode int
	AmbColor     string
	AmbEffect    int
	AmbBright    float64 // 0..1
	AmbSize      float64 // 0.5..2
	HoverEdit    bool    // move / resize a widget just by pointing at it on the desktop
	UITheme      int     // the settings window: 0 like Windows, 1 dark, 2 light
	Lang         string  // "" = Windows language, "en", "tr", "ru"
	// full screen (Cinema)
	FsHiRes         bool // sharp 2000 px covers from iTunes
	FsClock         bool
	FsFPS           int // 30 / 60 / 240 (= every refresh of the screen)
	FsScreen        int
	FsLyrics        bool
	FsStage         int // 1.21: the lyrics stage — 0 Lines (Apple-style), else styleNames index + 1
	FsBg            int // 1.22: Cinema's background (fsBgNames)
	FsCover         int // 1.22: the left side — 0 the cover, 2 a turning record (1, "big cover", was removed in 1.22.5)
	FsDisc          int // 1.22: the record's look — 0 like the Vinyl widget, else discNames index + 1
	FsArm           int // 1.22.2: the tonearm in full screen — 0 none, else fsArmNames index
	FsFont          string
	FsTextSize      int
	FsBold          bool
	FsEffect        int
	FsTheme         int
	FsTextColor     string
	FsAccentColor   string
	SpotifyClientID string // optional Spotify connection (the user's own app) → the real queue
	SpotifyRefresh  string
	SpotifyScopes   string // what the Spotify sign-in allows
	UpdateCheck     bool   // look for new versions on GitHub
	UpdateAuto      bool   // and put them in by themselves

	// halo
	ShowHalo bool
	// clock
	ShowClock      bool
	ClockStyle     int
	ClockFont      int     // font group
	ClockWeight    int     // 100..900
	ClockSpacing   float64 // letter spacing in em, -0.1..0.5
	ClockColorMode int     // solid / gradient / visualizer
	ClockColor     string
	ClockColor2    string
	Clock24h       bool
	ClockSeconds   bool
	ClockDate      bool
	ClockShadow    bool
	ClockGlow      bool
	ClockPulse     bool
	ClockDateLong  bool // "Monday, 21 September" instead of "Mon, 21 Sep"
	ClockAlign     int
	// weather
	ShowWeather    bool
	WeatherStyle   int
	WeatherCity    string // shown name, e.g. "Konya, Türkiye"
	WeatherLat     float64
	WeatherLon     float64
	WeatherF       bool // °F instead of °C
	WeatherAnimate bool
	WeatherDetails bool    // feels like · humidity · wind
	WeatherCard    float64 // 0..1 glass card behind it
	// screen glow
	ShowGlow       bool
	GlowStyle      int
	GlowWhere      int
	GlowMode       int     // follow the music / steady rhythm
	GlowSpeed      float64 // steady rhythm: pulses per second, 0.05..3
	GlowBright     float64 // 0.1..1
	GlowReach      float64 // 0.3..2.5 how far it reaches into the screen
	GlowColorMode  int
	GlowColor      string
	GlowFullscreen bool // keep glowing over full-screen games / video
	GlowScreens    int
	HaloBars       int
	HaloLength     float64
	HaloSens       float64
	HaloPulse      float64
	HaloSpin       bool
	HaloGlow       float64
	HaloStyle      int
	HaloPalette    int
	HaloColorA     string // custom gradient
	HaloColorB     string
	HaloBackdrop   float64 // 0..1 dark card behind it
	HaloArtGlow    bool    // album colours glow behind it
	HaloCard       int     // now playing card: off / compact / centred

	// performance
	FPS int // 0..60 (0 = no animation)

	Widgets map[string]*WidgetPos
	History []HistItem

	// v1 fields (migrated)
	LyricsSize int `json:",omitempty"`
	Font       int `json:",omitempty"`
}

const configVersion = 12

const appVersion = "1.23.9"

var legacyFonts = []string{"Consolas", "Cascadia Mono", "Bahnschrift", "Segoe UI", "Segoe UI Black", "Impact", "Georgia", "Courier New", "Segoe Script", "Ink Free", "Lucida Console", "Trebuchet MS"}

func defaultConfig() *Config {
	c := &Config{
		Version:     configVersion,
		UpdateCheck: true, UpdateAuto: true,
		ShowLyrics: true, LyricsStyle: stTypewriter, LyricsPlace: 0, TextSize: 40, FontName: "Consolas", Bold: true,
		Effect: fxSoftGlow, GlowAmount: 1, Theme: 0, TextColor: "#FFFFFF", AccentColor: "#E8B04A",
		ShowPrevLine: true, LyricsWidth: 46,
		ShowVinyl: true, DiscStyle: 0, VinylCorner: 0, SpinRPM: 12, ShowArm: true, ShowList: true, ListCount: 4,
		DiscColorMode: 1, DiscColor: "#C2185B", GrooveLight: true, DiscGlow: 1, Shine: true, ArmLift: true, ClickPlay: true,
		SwapAnim: true, Scratch: true, ScratchSec: 6,
		ShowHalo: true, HaloBars: 140, HaloLength: 1, HaloSens: 1, HaloPulse: 0.045, HaloSpin: false, HaloGlow: 1,
		ClockStyle: ckDigital, ClockWeight: 300, ClockColor: "#FFFFFF", ClockColor2: "#9B8CFF", Clock24h: true, ClockDate: true,
		ClockShadow: true, ClockDateLong: true, ClockAlign: 1,
		WeatherAnimate: true, WeatherDetails: true, WeatherCard: 0.6,
		AmbVinyl: true, AmbHalo: true, AmbColor: "#FF8A3D", AmbEffect: 1, AmbBright: 0.35, AmbSize: 1, HoverEdit: true,
		FsHiRes: true, FsClock: true, FsFPS: 60, FsLyrics: true, FsStage: 1 + stScatter, FsTheme: 1, FsArm: 1,
		FsFont: "Segoe UI", FsTextSize: 64, FsBold: true, FsEffect: fxSoftGlow, FsTextColor: "#FFFFFF", FsAccentColor: "#FFB86B",
		GlowStyle: glSoft, GlowSpeed: 0.5, GlowBright: 0.8, GlowReach: 1, GlowColor: "#7B5CFF", GlowColorMode: 2,
		HaloColorA: "#7B5CFF", HaloColorB: "#E15BFF", HaloCard: 0, MotionAmount: 1,
		FPS:     60,
		Widgets: map[string]*WidgetPos{},
	}
	c.validate()
	return c
}

var (
	cfg    = defaultConfig()
	cfgMu  sync.Mutex
	saveAt time.Time
)

func configPath() string { return filepath.Join(appDir(), "config.json") }

func loadConfig() {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return
	}
	c := defaultConfig()
	c.Version = 0 // a file without "Version" is treated as v1
	if json.Unmarshal(b, c) != nil {
		logf("config.json unreadable, using defaults")
		return
	}
	if c.Version < 2 {
		migrateV1(c, b)
	}
	if c.Version < 4 && c.Version > 0 && c.Theme == 7 {
		c.Theme = len(themes) - 1 // "Custom" moved to the end when new themes were added
	}
	if c.Version < 3 {
		// 1.1 saved the old 30 fps default before anyone could change it → use the new 60
		if c.FPS == 30 {
			c.FPS = 60
		}
		c.Version = 3
	}
	if c.Version > 0 && c.Version < 12 {
		c.FsStage, c.FsFPS = 1+stScatter, 60 // 1.21: the new lyrics stage, and full screen at 60 fps
	}
	c.Version = configVersion
	c.SpotifyRefresh = openSecret(c.SpotifyRefresh)
	if c.SpotifyRefresh == "" {
		c.SpotifyScopes = ""
	}
	c.validate()
	cfg = c
}

// configBytes: config.json's contents — secrets locked to this Windows user.
func configBytes(c *Config) []byte {
	cp := *c
	cp.SpotifyRefresh = sealSecret(c.SpotifyRefresh)
	b, _ := json.MarshalIndent(&cp, "", "  ")
	return b
}

// migrateV1 converts settings from the first release.
func migrateV1(c *Config, raw []byte) {
	var old struct {
		LyricsStyle, LyricsSize, Font int
		ShowPrevLine                  *bool
	}
	_ = json.Unmarshal(raw, &old)
	oldToNew := []int{stTypewriter, stCinematic, stKaraoke, stKinetic, stWordReel, stScatter, stSubtitle}
	if old.LyricsStyle >= 0 && old.LyricsStyle < len(oldToNew) {
		c.LyricsStyle = oldToNew[old.LyricsStyle]
	}
	c.TextSize = []float64{30, 40, 54, 72}[((old.LyricsSize%4)+4)%4]
	c.FontName = legacyFonts[((old.Font%len(legacyFonts))+len(legacyFonts))%len(legacyFonts)]
	c.LyricsSize, c.Font = 0, 0
	c.Version = configVersion
}

var reHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func clampF(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Max(lo, math.Min(hi, v))
}
func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// validate keeps every value in range (also protects against hand-edited config files).
func (c *Config) validate() {
	c.validateFields()
	c.validateWidgets()
}

// validateFields never touches the Widgets map or History (they are shared with the UI thread).
func (c *Config) validateFields() {
	c.LyricsStyle = clampI(c.LyricsStyle, 0, len(styleNames)-1)
	c.LyricsPlace = clampI(c.LyricsPlace, 0, 1)
	c.TextSize = clampF(c.TextSize, 14, 160)
	if c.FontName == "" || len(c.FontName) > 64 {
		c.FontName = "Consolas"
	}
	c.Effect = clampI(c.Effect, 0, len(effectNames)-1)
	c.GlowAmount = clampF(c.GlowAmount, 0, 2)
	c.Theme = clampI(c.Theme, 0, len(themes)-1)
	if !reHex.MatchString(c.TextColor) {
		c.TextColor = "#FFFFFF"
	}
	if !reHex.MatchString(c.AccentColor) {
		c.AccentColor = "#E8B04A"
	}
	c.LyricsWidth = clampF(c.LyricsWidth, 20, 90)
	c.OffsetSec = clampF(c.OffsetSec, -10, 10)
	c.DiscStyle = clampI(c.DiscStyle, 0, len(discNames)-1)
	c.VinylCorner = clampI(c.VinylCorner, 0, 3)
	c.SpinRPM = clampF(c.SpinRPM, 0, 78)
	c.ScratchSec = clampF(c.ScratchSec, 2, 30)
	c.SwapStyle = clampI(c.SwapStyle, 0, len(swapStyleNames)-1)
	c.VinylNext = clampI(c.VinylNext, 0, 1)
	if c.Upper && c.TextCase == 0 {
		c.TextCase = 1
	}
	c.Upper = false
	c.TextCase = clampI(c.TextCase, 0, len(caseNames)-1)
	c.Motion = clampI(c.Motion, 0, len(motionNames)-1)
	c.StarShape = clampI(c.StarShape, 0, len(starShapeNames)-1)
	c.MotionAmount = clampF(c.MotionAmount, 0, 2)
	c.HaloStyle = clampI(c.HaloStyle, 0, len(visNames)-1)
	c.HaloPalette = clampI(c.HaloPalette, 0, len(visPalettes)-1)
	if !reHex.MatchString(c.HaloColorA) {
		c.HaloColorA = "#7B5CFF"
	}
	if !reHex.MatchString(c.HaloColorB) {
		c.HaloColorB = "#E15BFF"
	}
	c.HaloBackdrop = clampF(c.HaloBackdrop, 0, 1)
	c.HaloCard = clampI(c.HaloCard, 0, len(visCardNames)-1)
	c.ClockStyle = clampI(c.ClockStyle, 0, len(clockStyleNames)-1)
	c.ClockFont = clampI(c.ClockFont, 0, len(fontGroupNames)-1)
	c.ClockWeight = clampI((c.ClockWeight+50)/100*100, 100, 900)
	c.ClockSpacing = clampF(c.ClockSpacing, -0.1, 0.5)
	c.ClockColorMode = clampI(c.ClockColorMode, 0, len(clockColorNames)-1)
	if !reHex.MatchString(c.ClockColor) {
		c.ClockColor = "#FFFFFF"
	}
	if !reHex.MatchString(c.ClockColor2) {
		c.ClockColor2 = "#9B8CFF"
	}
	c.ClockAlign = clampI(c.ClockAlign, 0, len(alignNames)-1)
	c.WeatherStyle = clampI(c.WeatherStyle, 0, len(weatherStyleNames)-1)
	if len([]rune(c.WeatherCity)) > 80 {
		c.WeatherCity = string([]rune(c.WeatherCity)[:80])
	}
	c.WeatherLat = clampF(c.WeatherLat, -90, 90)
	c.WeatherLon = clampF(c.WeatherLon, -180, 180)
	c.WeatherCard = clampF(c.WeatherCard, 0, 1)
	c.GlowStyle = clampI(c.GlowStyle, 0, len(glowStyleNames)-1)
	c.GlowWhere = clampI(c.GlowWhere, 0, len(glowWhereNames)-1)
	c.GlowMode = clampI(c.GlowMode, 0, len(glowModeNames)-1)
	c.GlowSpeed = clampF(c.GlowSpeed, 0.05, 3)
	c.GlowBright = clampF(c.GlowBright, 0.1, 1)
	c.GlowReach = clampF(c.GlowReach, 0.3, 2.5)
	c.GlowColorMode = clampI(c.GlowColorMode, 0, len(glowColorNames)-1)
	if !reHex.MatchString(c.GlowColor) {
		c.GlowColor = "#7B5CFF"
	}
	c.GlowScreens = clampI(c.GlowScreens, 0, len(glowScreenNames)-1)
	c.DiscColorMode = clampI(c.DiscColorMode, 0, len(discColorNames)-1)
	if !reHex.MatchString(c.DiscColor) {
		c.DiscColor = "#C2185B"
	}
	c.DiscGlow = clampF(c.DiscGlow, 0, 2)
	c.VinylLayout = clampI(c.VinylLayout, 0, len(vinylLayoutNames)-1)
	c.AmbColorMode = clampI(c.AmbColorMode, 0, len(ambColorNames)-1)
	c.AmbEffect = clampI(c.AmbEffect, 0, len(ambEffectNames)-1)
	if !reHex.MatchString(c.AmbColor) {
		c.AmbColor = "#FF8A3D"
	}
	c.AmbBright = clampF(c.AmbBright, 0, 1)
	c.AmbSize = clampF(c.AmbSize, 0.5, 2)
	if c.FsFPS != 30 && c.FsFPS != 240 { // 240: as fast as the screen refreshes (up to 240 Hz)
		c.FsFPS = 60
	}
	c.FsScreen = clampI(c.FsScreen, 0, len(fsScreenNames)-1)
	c.FsStage = clampI(c.FsStage, 0, len(styleNames))
	c.FsBg = clampI(c.FsBg, 0, len(fsBgNames)-1)
	c.FsCover = clampI(c.FsCover, 0, len(fsCoverNames)-1)
	if c.FsCover == 1 { // 1.22.5: "Big cover" is gone — the cover it is
		c.FsCover = 0
	}
	c.FsDisc = clampI(c.FsDisc, 0, len(discNames))
	c.FsArm = clampI(c.FsArm, 0, len(fsArmNames)-1)
	if strings.TrimSpace(c.FsFont) == "" || len(c.FsFont) > 100 {
		c.FsFont = "Segoe UI"
	}
	c.FsTextSize = clampI(c.FsTextSize, 20, 200)
	c.FsEffect = clampI(c.FsEffect, 0, len(effectNames)-1)
	c.FsTheme = clampI(c.FsTheme, 0, len(themes)-1)
	if !reHex.MatchString(c.FsTextColor) {
		c.FsTextColor = "#FFFFFF"
	}
	if !reHex.MatchString(c.FsAccentColor) {
		c.FsAccentColor = "#FFB86B"
	}
	c.UITheme = clampI(c.UITheme, 0, len(uiThemeNames)-1)
	if !validLang(c.Lang) {
		c.Lang = ""
	}
	c.SpotifyClientID = strings.TrimSpace(c.SpotifyClientID)
	if len(c.SpotifyClientID) > 64 || strings.ContainsAny(c.SpotifyClientID, " /?&#\\\"'<>") {
		c.SpotifyClientID = ""
	}
	c.ListCount = clampI(c.ListCount, 0, 6)
	c.HaloBars = clampI(c.HaloBars, 24, 240)
	c.HaloLength = clampF(c.HaloLength, 0.2, 2.5)
	c.HaloSens = clampF(c.HaloSens, 0.2, 3)
	c.HaloPulse = clampF(c.HaloPulse, 0, 0.2)
	c.HaloGlow = clampF(c.HaloGlow, 0, 2)
	c.FPS = clampI(c.FPS, 0, 60)
}

func (c *Config) validateWidgets() {
	if c.Widgets == nil {
		c.Widgets = map[string]*WidgetPos{}
	}
	for k, w := range c.Widgets {
		if w == nil {
			delete(c.Widgets, k)
			continue
		}
		if w.Scale <= 0 {
			w.Scale = 1
		}
		w.Scale = clampF(w.Scale, 0.3, 3)
		if w.Opacity != 0 {
			w.Opacity = clampF(w.Opacity, 0.15, 1)
		}
	}
	for _, k := range allWidgets { // pre-create so the map is never written while drawing
		if c.Widgets[k] == nil {
			c.Widgets[k] = &WidgetPos{Scale: 1}
		}
	}
	keep := c.History[:0]
	for _, h := range c.History {
		if !looksLikeID(h.Title) { // hash "titles" from earlier versions
			keep = append(keep, h)
		}
	}
	c.History = keep
	if len(c.History) > 8 {
		c.History = c.History[:8]
	}
}

// setField changes one setting by its JSON name (used by the settings window).
func (c *Config) setField(key string, value json.RawMessage) error {
	switch key {
	case "Widgets", "History", "Version", "LyricsSize", "Font", "SpotifyRefresh", "SpotifyScopes": // (the Spotify sign-in only comes from Spotify)
		return fmt.Errorf("%s cannot be set", key)
	}
	// Work on a copy WITHOUT the maps/slices: decoding into the shared Widgets map from the
	// settings server while the UI thread reads it crashed version 1.1.
	base := *c
	base.Widgets, base.History = nil, nil
	b, _ := json.Marshal(base)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	if _, ok := m[key]; !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	m[key] = value
	delete(m, "Widgets")
	delete(m, "History")
	b, _ = json.Marshal(m)
	var n Config
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("bad value for %s: %v", key, err)
	}
	n.validateFields()
	if n.SpotifyClientID != c.SpotifyClientID { // another Spotify app: the old sign-in isn't for it
		n.SpotifyRefresh, n.SpotifyScopes = "", ""
	}
	n.Widgets, n.History = c.Widgets, c.History
	*c = n
	return nil
}

// saveConfigSoon batches writes (positions change many times while dragging).
func saveConfigSoon() { saveAt = time.Now().Add(800 * time.Millisecond) }

func saveConfigIfDue(force bool) {
	if saveAt.IsZero() || (!force && time.Now().Before(saveAt)) {
		return
	}
	saveAt = time.Time{}
	cfgMu.Lock()
	b := configBytes(cfg)
	cfgMu.Unlock()
	tmp := configPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, configPath())
	}
}

func (c *Config) widget(name string) *WidgetPos {
	w := c.Widgets[name]
	if w == nil {
		w = &WidgetPos{Scale: 1}
		c.Widgets[name] = w
	}
	if w.Scale <= 0 {
		w.Scale = 1
	}
	return w
}

func parseHex(s string) uint32 {
	var v uint32
	fmt.Sscanf(s, "#%06x", &v)
	return 0xFF000000 | v
}

/* ---------- Log file ---------- */

var logMu sync.Mutex

func logf(format string, a ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	p := filepath.Join(appDir(), "orbit.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 512*1024 {
		_ = os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
}
