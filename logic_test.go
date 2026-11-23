package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "orbit")
	if d := os.Getenv("ORBIT_APPDATA"); d != "" { // browser tests restart the server with the same key
		dir = d
	}
	os.Setenv("APPDATA", dir)
	code := m.Run()
	if os.Getenv("ORBIT_APPDATA") == "" {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

func TestParseLRC(t *testing.T) {
	ls := parseLRC("[ar:x]\n[00:01.50]Hello world\n[00:03.00][00:20.00]Repeat me\n[00:05.25]\n[offset:+500]\n[00:07.00]Later")
	if len(ls) != 5 {
		t.Fatalf("lines = %d", len(ls))
	}
	if ls[0].T != 1.5 || ls[0].Text != "Hello world" {
		t.Fatalf("first = %+v", ls[0])
	}
	if ls[len(ls)-1].Text != "Repeat me" || ls[len(ls)-1].T != 20 {
		t.Fatalf("repeated tag not sorted: %+v", ls[len(ls)-1])
	}
	if ls[3].Text != "Later" || math.Abs(ls[3].T-6.5) > 1e-9 {
		t.Fatalf("offset not applied: %+v", ls[3])
	}
	// enhanced (word-timed) LRC
	w := parseLRC("[00:10.00]<00:10.00> Hey <00:10.50> there <00:11.20> you")
	if len(w[0].Words) != 3 || w[0].Words[2].T != 11.2 || w[0].Text != "Hey there you" {
		t.Fatalf("word tags: %+v", w[0])
	}
}

func TestPrepareTiming(t *testing.T) {
	ls := prepare(parseLRC("[00:01.00]one two three four\n[00:04.00]next line here\n[00:30.00]end"))
	L := ls[0]
	if len(L.Words) != 4 || L.Words[0].T != 1 {
		t.Fatalf("words %+v", L.Words)
	}
	for i := 1; i < 4; i++ {
		if L.Words[i].T <= L.Words[i-1].T || L.Words[i].T >= L.End {
			t.Fatalf("word times not increasing inside the line: %+v", L.Words)
		}
	}
	if L.Sing <= 0 || L.Sing > L.End-L.T {
		t.Fatalf("sing %v", L.Sing)
	}
	if lineAt(ls, 0.5) != -1 || lineAt(ls, 1) != 0 || lineAt(ls, 5) != 1 || lineAt(ls, 99) != 2 {
		t.Fatal("lineAt")
	}
	if wordAt(&ls[0], 0.9) != -1 || wordAt(&ls[0], 1.0) != 0 || wordAt(&ls[0], 3.9) != 3 {
		t.Fatal("wordAt")
	}
	empty := prepare(parseLRC("[00:01.00]\n[00:02.00]x"))
	if len(empty[0].Words) != 0 {
		t.Fatal("empty line should have no words")
	}
}

func TestPlainToLines(t *testing.T) {
	ls := plainToLines("a\n\nb\nc\n", 100)
	if len(ls) != 3 || ls[0].T != 8 || ls[2].T <= ls[1].T {
		t.Fatalf("%+v", ls)
	}
}

func TestKeys(t *testing.T) {
	cases := [][2]string{
		{keyOf("Swim", "Chase Atlantic"), keyOf("swim", "Chase Atlantic, Someone")},
		{keyOf("Pentru totdeauna (Remastered)", "Irina Rimes"), keyOf("Pentru totdeauna", "Irina Rimes & Grasu XXL")},
		{keyOf("Gülümse - Live", "Sezen Aksu"), keyOf("Gulumse", "SEZEN AKSU")},
		{keyOf("Şımarık", "Tarkan feat. X"), keyOf("Simarik", "Tarkan")},
	}
	for i, c := range cases {
		if c[0] != c[1] {
			t.Errorf("case %d: %q != %q", i, c[0], c[1])
		}
	}
	if keyOf("Лампочки", "ARTIK & ASTI") == "|" || !strings.Contains(keyOf("Лампочки", "ARTIK & ASTI"), "лампочки") {
		t.Errorf("cyrillic lost: %q", keyOf("Лампочки", "ARTIK & ASTI"))
	}
	if cleanTitle("Swim (feat. X) - 2019 Remaster") != "Swim" {
		t.Errorf("cleanTitle %q", cleanTitle("Swim (feat. X) - 2019 Remaster"))
	}
}

func TestPickBest(t *testing.T) {
	s := func(v string) *string { return &v }
	arr := []lrcRec{
		{TrackName: "Swim", ArtistName: "Other", Duration: 100, PlainLyrics: s("x")},
		{TrackName: "Swim", ArtistName: "Chase Atlantic", Duration: 215, SyncedLyrics: s("[00:01.00]a")},
		{TrackName: "Swimming", ArtistName: "Chase Atlantic", Duration: 215, SyncedLyrics: s("[00:01.00]b")},
	}
	b := pickBest(arr, "Swim", "Chase Atlantic", 214)
	if b == nil || b.ArtistName != "Chase Atlantic" || b.TrackName != "Swim" {
		t.Fatalf("picked %+v", b)
	}
	if pickBest([]lrcRec{{TrackName: "x", ArtistName: "y"}}, "Swim", "Chase", 0) != nil {
		t.Fatal("should reject records without lyrics")
	}
}

func TestLookupWithServer(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		if strings.HasPrefix(r.URL.Path, "/api/get") {
			if r.URL.Query().Get("duration") != "215" {
				t.Errorf("duration %q", r.URL.Query().Get("duration"))
			}
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{{"trackName": "Swim", "artistName": "Chase Atlantic", "duration": 215, "syncedLyrics": "[00:01.00]Hello"}})
	}))
	defer srv.Close()
	lrclibBase = srv.URL
	defer func() { lrclibBase = "https://lrclib.net" }()
	tr := Track{Key: keyOf("Swim", "Chase Atlantic"), Title: "Swim", Artist: "Chase Atlantic", Duration: 215.4}
	c, err := lookupLyrics(tr)
	if err != nil || c == nil || c.Synced != "[00:01.00]Hello" {
		t.Fatalf("lookup %+v %v", c, err)
	}
	before := hits
	c2, _ := lookupLyrics(tr) // must come from the cache
	if hits != before || c2.Synced != c.Synced {
		t.Fatal("cache not used")
	}
	// not found is cached as such
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("[]")) }))
	defer srv2.Close()
	lrclibBase = srv2.URL
	nf, err := lookupLyrics(Track{Key: "nothing|here", Title: "Nothing", Artist: "Here"})
	if err != nil || !nf.NotFound {
		t.Fatalf("not found: %+v %v", nf, err)
	}
	// network error is reported, not cached as "not found"
	lrclibBase = "http://127.0.0.1:1"
	if _, err := lookupLyrics(Track{Key: "net|err", Title: "Net", Artist: "Err"}); err == nil {
		t.Fatal("expected network error")
	}
}

func TestLyricsLoadAsync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/get") {
			json.NewEncoder(w).Encode(map[string]any{"trackName": "A", "artistName": "B", "duration": 100, "syncedLyrics": "[00:01.00]one\n[00:02.00]two"})
			return
		}
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	lrclibBase = srv.URL
	defer func() { lrclibBase = "https://lrclib.net" }()
	l := &Lyrics{}
	l.Load(&Track{Key: "a|b-async", Title: "A", Artist: "B", Duration: 100})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, st, ls := l.Get(); st == lyrSynced && len(ls) == 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, st, _ := l.Get()
	t.Fatalf("status %d", st)
}

func TestWrapAndBalance(t *testing.T) {
	widths := []float64{50, 30, 80, 40, 60, 20, 70}
	for _, maxW := range []float64{100, 150, 250, 1000} {
		rows := balanceRows(widths, 10, maxW)
		n := 0
		for _, r := range rows {
			w := 0.0
			for k, i := range r {
				if i != n {
					t.Fatalf("words out of order")
				}
				n++
				w += widths[i]
				if k > 0 {
					w += 10
				}
			}
			if w > maxW && len(r) > 1 {
				t.Fatalf("row too wide %v > %v", w, maxW)
			}
		}
		if n != len(widths) {
			t.Fatalf("lost words")
		}
	}
	// a single word wider than maxW still gets its own row
	if r := wrapWords([]float64{500}, 10, 100); len(r) != 1 {
		t.Fatal("wide word")
	}
}

func TestScatterAndRandomSpot(t *testing.T) {
	for n := 1; n < 20; n++ {
		sp := scatterLayout(n, int64(n), 800, 300)
		seen := map[[2]int]bool{}
		for _, s := range sp {
			if s.X < 0 || s.X > 800 || s.Y < 0 || s.Y > 300 || s.Scale < 0.6 || s.Scale > 1.2 {
				t.Fatalf("spot out of box %+v", s)
			}
			k := [2]int{int(s.X / 10), int(s.Y / 10)}
			if seen[k] {
				t.Fatalf("two words on the same spot")
			}
			seen[k] = true
		}
		again := scatterLayout(n, int64(n), 800, 300)
		if again[0] != sp[0] {
			t.Fatal("not stable for the same seed")
		}
	}
	r := rand.New(rand.NewSource(1))
	area := rectI{0, 0, 1920, 1040}
	avoid := []rectI{{0, 600, 700, 1040}, {760, 300, 1160, 700}}
	last := [2]int{0, 0}
	for i := 0; i < 300; i++ {
		x, y := randomSpot(r, area, 600, 160, avoid, last)
		b := rectI{x, y, x + 600, y + 160}
		if b.L < area.L || b.T < area.T || b.R > area.R || b.B > area.B {
			t.Fatalf("outside area %+v", b)
		}
		for _, a := range avoid {
			if b.overlaps(a) {
				t.Fatalf("overlaps vinyl/halo %+v", b)
			}
		}
		last = [2]int{x, y}
	}
	// box bigger than the area must not panic
	randomSpot(r, rectI{0, 0, 100, 100}, 300, 300, nil, last)
}

func TestArmAngle(t *testing.T) {
	R := 200.0
	cx, cy := 216.0, 224.0
	pa := -57 * math.Pi / 180
	px, py := cx+R*1.02*math.Cos(pa), cy+R*1.02*math.Sin(pa)
	L := R * 0.74
	for _, prog := range []float64{0, 0.5, 1} {
		rt := R * (0.93 - 0.27*prog)
		th := armAngle(px, py, cx, cy, L, rt)
		sx, sy := px+L*math.Cos(th), py+L*math.Sin(th)
		if d := math.Hypot(sx-cx, sy-cy); math.Abs(d-rt) > 0.5 {
			t.Fatalf("stylus at %v, want %v", d, rt)
		}
		if sy > cy-R*0.3 {
			t.Fatalf("stylus should sit over the top part of the record (y=%v)", sy-cy)
		}
		// the label (cover) has radius 0.64R: the stylus must stay on the grooves
		if math.Hypot(sx-cx, sy-cy) < R*0.64 {
			t.Fatalf("stylus on the label")
		}
	}
}

func TestAccent(t *testing.T) {
	red := make([]uint32, 100)
	for i := range red {
		red[i] = 0xFFC81E1E
		if i%3 == 0 {
			red[i] = 0xFF101010
		}
	}
	a := accentFromPixels(red)
	r, g, b := float64(a>>16&255), float64(a>>8&255), float64(a&255)
	h, _, l := rgbToHsl(r, g, b)
	if !(h < 0.05 || h > 0.95) || l < 0.6 {
		t.Fatalf("red cover gave %08X", a)
	}
	gray := []uint32{0xFFEEEEEE, 0xFF888888, 0xFF222222}
	if a := accentFromPixels(gray); a>>24 != 0xFF {
		t.Fatal("alpha")
	}
}

func TestFFTAndSpectrum(t *testing.T) {
	s := &Spectrum{rate: 48000, peak: 1e-3}
	buf := make([]float32, 4096)
	for i := range buf {
		buf[i] = float32(0.5 * math.Sin(2*math.Pi*100*float64(i)/48000))
	}
	var bands [nBands]float32
	for k := 0; k < 5; k++ {
		s.Feed(buf, 48000)
		bands, _, _ = s.Compute()
	}
	maxI := 0
	for i := range bands {
		if bands[i] > bands[maxI] {
			maxI = i
		}
	}
	if maxI > 10 {
		t.Fatalf("100 Hz tone peaked in band %d", maxI)
	}
	hi := make([]float32, 4096)
	for i := range hi {
		hi[i] = float32(0.5 * math.Sin(2*math.Pi*5000*float64(i)/48000))
	}
	s2 := &Spectrum{rate: 48000, peak: 1e-3}
	for k := 0; k < 5; k++ {
		s2.Feed(hi, 48000)
		bands, _, _ = s2.Compute()
	}
	maxI = 0
	for i := range bands {
		if bands[i] > bands[maxI] {
			maxI = i
		}
	}
	if maxI < 35 {
		t.Fatalf("5 kHz tone peaked in band %d", maxI)
	}
	// silence decays to zero
	s3 := &Spectrum{rate: 48000, peak: 1e-3}
	s3.Feed(make([]float32, 2048), 48000)
	b3, _, active := s3.Compute()
	if active || b3[0] != 0 {
		t.Fatal("silence should be inactive")
	}
}

func TestMediaState(t *testing.T) {
	m := &Media{}
	if !m.SetTrack("Swim", "Chase Atlantic", "", "smtc") || m.SetTrack("swim", "Chase Atlantic", "", "smtc") {
		t.Fatal("same song detection")
	}
	v := m.Snapshot().Version
	m.SetPosition(30, true)
	time.Sleep(120 * time.Millisecond)
	if p := m.Snapshot().Pos; p < 30.1 || p > 30.4 {
		t.Fatalf("playing position %v", p)
	}
	m.SetPlaying(false)
	p1 := m.Snapshot().Pos
	time.Sleep(100 * time.Millisecond)
	if m.Snapshot().Pos != p1 {
		t.Fatal("paused position moved")
	}
	m.SetPlaying(true)
	time.Sleep(60 * time.Millisecond)
	if p := m.Snapshot().Pos; p < p1+0.04 || p > p1+0.2 {
		t.Fatalf("resume continuity %v -> %v", p1, p)
	}
	m.SetDuration(keyOf("Swim", "Chase Atlantic"), 31)
	time.Sleep(1100 * time.Millisecond)
	if p := m.Snapshot().Pos; p > 31 {
		t.Fatalf("position beyond duration %v", p)
	}
	m.SetOffset(0.5)
	if p := m.Snapshot().Pos; p != 31.5 {
		t.Fatalf("offset %v", p)
	}
	m.SetCover("wrong|key", []byte{1})
	if m.Snapshot().Track.Cover != nil {
		t.Fatal("cover for another song accepted")
	}
	m.SetCover(keyOf("Swim", "Chase Atlantic"), []byte{1})
	if m.Snapshot().Track.CoverVer != 1 {
		t.Fatal("cover")
	}
	m.SetTrack("", "", "", "")
	if s := m.Snapshot(); s.Track != nil || s.Version == v {
		t.Fatal("clear")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	cfg = defaultConfig()
	cfg.LyricsStyle, cfg.FontName, cfg.Theme, cfg.FPS = stScatter, "Georgia", 2, 45
	cfg.widget("vinyl").X = -60
	cfg.widget("vinyl").Set = true
	cfg.History = []HistItem{{Key: "a|b", Title: "A", Artist: "B"}}
	saveConfigSoon()
	saveConfigIfDue(true)
	cfg = defaultConfig()
	loadConfig()
	if cfg.LyricsStyle != stScatter || cfg.FontName != "Georgia" || cfg.Theme != 2 || cfg.FPS != 45 || cfg.widget("vinyl").X != -60 || len(cfg.History) != 1 {
		t.Fatalf("config %+v", cfg)
	}
	os.WriteFile(configPath(), []byte("{broken"), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.LyricsStyle != stTypewriter || cfg.FPS != 60 {
		t.Fatal("broken config should fall back to defaults")
	}
	if !cfg.ShowHalo || !cfg.ShowVinyl || !cfg.ShowLyrics {
		t.Fatal("defaults")
	}
	for _, k := range []string{"lyrics", "vinyl", "halo"} {
		if cfg.Widgets[k] == nil {
			t.Fatalf("widget %s not pre-created", k)
		}
	}
}

func TestMigrateV1(t *testing.T) {
	// a config written by version 1.0 (indexes into the old 7-style / 12-font lists)
	old := `{"LyricsStyle":3,"LyricsPlace":1,"LyricsSize":2,"Font":5,"Bold":false,"Upper":true,"Theme":2,"ShowLyrics":true,"ShowVinyl":true,"ShowList":false,"ShowHalo":false,"ShowPrevLine":true,"OffsetSec":1.5,"Widgets":{"vinyl":{"X":10,"Y":20,"Scale":1.4,"Set":true}},"History":[]}`
	os.WriteFile(configPath(), []byte(old), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.Version != configVersion || cfg.LyricsStyle != stKinetic || cfg.TextSize != 54 || cfg.FontName != "Impact" {
		t.Fatalf("migration: %+v", cfg)
	}
	if cfg.Bold || cfg.TextCase != 1 || cfg.ShowList || cfg.ShowHalo || cfg.LyricsPlace != 1 || cfg.OffsetSec != 1.5 || cfg.widget("vinyl").Scale != 1.4 {
		t.Fatalf("kept values lost: %+v", cfg)
	}
	if cfg.FPS != 60 || cfg.Effect != fxSoftGlow || cfg.HaloBars != 140 {
		t.Fatalf("new fields should get defaults: %+v", cfg)
	}
	oldStyles := map[int]int{0: stTypewriter, 1: stCinematic, 2: stKaraoke, 3: stKinetic, 4: stWordReel, 5: stScatter, 6: stSubtitle}
	for o, n := range oldStyles {
		os.WriteFile(configPath(), []byte(fmt.Sprintf(`{"LyricsStyle":%d}`, o)), 0o644)
		cfg = defaultConfig()
		loadConfig()
		if cfg.LyricsStyle != n {
			t.Errorf("old style %d → %d, want %d", o, cfg.LyricsStyle, n)
		}
	}
}

func TestMigrateV2FPS(t *testing.T) {
	os.WriteFile(configPath(), []byte(`{"Version":2,"FPS":30,"FontName":"Georgia"}`), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.FPS != 60 || cfg.FontName != "Georgia" || cfg.Version != configVersion {
		t.Fatalf("v2 → v3: %+v", cfg)
	}
	os.WriteFile(configPath(), []byte(`{"Version":2,"FPS":24}`), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.FPS != 24 {
		t.Fatal("a chosen fps must be kept")
	}
}

func TestSetFieldValidation(t *testing.T) {
	c := defaultConfig()
	ok := map[string]string{"FPS": "0", "TextSize": "88.5", "FontName": `"Bahnschrift"`, "Effect": "6", "LyricsStyle": "11", "ShowArm": "false", "TextColor": `"#12AB9F"`, "VinylCorner": "3", "HaloBars": "24", "OffsetSec": "-2.5"}
	for k, v := range ok {
		if err := c.setField(k, json.RawMessage(v)); err != nil {
			t.Errorf("%s=%s: %v", k, v, err)
		}
	}
	if c.FPS != 0 || c.TextSize != 88.5 || c.FontName != "Bahnschrift" || c.Effect != 6 || c.ShowArm || c.TextColor != "#12AB9F" || c.VinylCorner != 3 || c.OffsetSec != -2.5 {
		t.Fatalf("values not applied: %+v", c)
	}
	clamped := map[string]string{"FPS": "500", "TextSize": "3", "Effect": "99", "LyricsStyle": "-4", "HaloBars": "100000", "GlowAmount": "9", "TextColor": `"red"`, "SpinRPM": "-1"}
	for k, v := range clamped {
		if err := c.setField(k, json.RawMessage(v)); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	if c.FPS != 60 || c.TextSize != 14 || c.Effect != len(effectNames)-1 || c.LyricsStyle != 0 || c.HaloBars != 240 || c.GlowAmount != 2 || c.TextColor != "#FFFFFF" || c.SpinRPM != 0 {
		t.Fatalf("not clamped: %+v", c)
	}
	for _, bad := range [][2]string{{"Nope", "1"}, {"Widgets", "{}"}, {"History", "[]"}, {"FPS", `"fast"`}, {"ShowArm", `"yes"`}} {
		if err := c.setField(bad[0], json.RawMessage(bad[1])); err == nil {
			t.Errorf("%s=%s should fail", bad[0], bad[1])
		}
	}
}

func TestSettingsServer(t *testing.T) {
	cfg = defaultConfig()
	var edit bool
	var reset string
	changed := []string{}
	hooks.SetEditMode = func(b bool) { edit = b }
	hooks.ResetPositions = func(w string) { reset = w }
	hooks.SettingsChanged = func(k string) { changed = append(changed, k) }
	hooks.Fonts = func() []string { return []string{"Arial", "Consolas"} }
	srv, err := startSettingsServer("")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", srv.port)
	get := func(path string) (*http.Response, string) {
		r, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		b := new(strings.Builder)
		io.Copy(b, r.Body)
		r.Body.Close()
		return r, b.String()
	}
	post := func(path, body string) int {
		r, err := http.Post(base+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if r, _ := get("/?t=wrong"); r.StatusCode != 403 {
		t.Fatal("wrong token must be refused")
	}
	if r, _ := get("/api/state"); r.StatusCode != 403 {
		t.Fatal("missing token must be refused")
	}
	// other host names (DNS rebinding) are refused
	req, _ := http.NewRequest("GET", base+"/api/state?t="+srv.token, nil)
	req.Host = "evil.example.com"
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 403 {
		t.Fatal("foreign Host must be refused")
	}
	r, page := get("/?t=" + srv.token)
	if r.StatusCode != 200 || !strings.Contains(page, "<title>Orbit — Settings</title>") {
		t.Fatal("page")
	}
	_, st := get("/api/state?t=" + srv.token)
	var state map[string]any
	if json.Unmarshal([]byte(st), &state) != nil || len(state["styles"].([]any)) != len(styleNames) || len(state["fonts"].([]any)) != 2 {
		t.Fatalf("state %s", st)
	}
	tok := "?t=" + srv.token
	if c := post("/api/set"+tok, `{"key":"FPS","value":12}`); c != 200 {
		t.Fatalf("set %d", c)
	}
	if c := post("/api/set"+tok, `{"key":"Bogus","value":1}`); c != 400 {
		t.Fatalf("bogus key %d", c)
	}
	if c := post("/api/set"+tok, `{"key":"FPS","value":"x"}`); c != 400 {
		t.Fatalf("bad value %d", c)
	}
	if c := post("/api/set"+tok, `not json`); c != 400 {
		t.Fatalf("bad json %d", c)
	}
	if cfg.FPS == 12 {
		t.Fatal("change must wait for the UI thread")
	}
	drainUI()
	if cfg.FPS != 12 || len(changed) != 1 || changed[0] != "FPS" {
		t.Fatalf("after drain fps=%d changed=%v", cfg.FPS, changed)
	}
	post("/api/action"+tok, `{"name":"editMode","value":true}`)
	post("/api/action"+tok, `{"name":"resetPositions","value":"vinyl"}`)
	if c := post("/api/action"+tok, `{"name":"rm -rf"}`); c != 400 {
		t.Fatal("unknown action")
	}
	drainUI()
	if !edit || reset != "vinyl" {
		t.Fatalf("actions edit=%v reset=%q", edit, reset)
	}
	// widget flags, opacity, show-me — and their validation
	var flags []string
	var opac float64
	var flashed string
	hooks.SetWidgetFlag = func(n, f string, on bool) { flags = append(flags, fmt.Sprintf("%s/%s/%v", n, f, on)) }
	hooks.SetOpacity = func(n string, v float64) { opac = v }
	hooks.Flash = func(n string) { flashed = n }
	ok := []string{`{"name":"widgetFlag","value":{"name":"vinyl","flag":"onTop","on":true}}`, `{"name":"widgetFlag","value":{"name":"halo","flag":"locked","on":true}}`,
		`{"name":"opacity","value":{"name":"lyrics","v":0.01}}`, `{"name":"flash","value":"halo"}`}
	for _, b := range ok {
		if c := post("/api/action"+tok, b); c != 200 {
			t.Fatalf("%s → %d", b, c)
		}
	}
	bad := []string{`{"name":"widgetFlag","value":{"name":"vinyl","flag":"rm","on":true}}`, `{"name":"widgetFlag","value":{"name":"x","flag":"onTop","on":true}}`,
		`{"name":"opacity","value":{"name":"desktop","v":1}}`, `{"name":"opacity","value":"x"}`, `{"name":"flash","value":"../etc"}`}
	for _, b := range bad {
		if c := post("/api/action"+tok, b); c != 400 {
			t.Fatalf("%s should be refused, got %d", b, c)
		}
	}
	drainUI()
	if strings.Join(flags, ",") != "vinyl/onTop/true,halo/locked/true" || opac != 0.15 || flashed != "halo" {
		t.Fatalf("flags=%v opacity=%v flash=%q", flags, opac, flashed)
	}
	// cover: 404 with nothing playing, the image bytes when there is one, never without the token
	media.SetTrack("T", "A", "", "test")
	if r, _ := get("/api/cover" + tok); r.StatusCode != 404 {
		t.Fatal("no cover → 404")
	}
	png := []byte("\x89PNG\r\n\x1a\n0000000000000000")
	media.SetCover(media.Snapshot().Track.Key, png)
	if r, body := get("/api/cover" + tok); r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || body != string(png) {
		t.Fatalf("cover %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}
	if r, _ := get("/api/cover"); r.StatusCode != 403 {
		t.Fatal("cover without token")
	}
	media.SetCover(media.Snapshot().Track.Key, []byte("<html>not an image</html>"))
	if r, _ := get("/api/cover" + tok); r.StatusCode != 404 {
		t.Fatal("non-image cover bytes must not be served")
	}
}

func TestV6Settings(t *testing.T) {
	// a 1.5 user with ALL CAPS on keeps it as TextCase
	os.WriteFile(configPath(), []byte(`{"Version":5,"Upper":true,"Theme":3}`), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.TextCase != 1 || cfg.Upper || cfg.Theme != 3 {
		t.Fatalf("upper → case: %+v", cfg.TextCase)
	}
	// every new option accepts its full range and clamps the rest
	c := defaultConfig()
	for k, n := range map[string]int{"HaloStyle": len(visNames), "HaloPalette": len(visPalettes), "HaloCard": len(visCardNames), "Motion": len(motionNames),
		"TextCase": len(caseNames), "DiscStyle": len(discNames), "DiscColorMode": len(discColorNames)} {
		for i := 0; i < n; i++ {
			if err := c.setField(k, json.RawMessage(strconv.Itoa(i))); err != nil {
				t.Fatalf("%s=%d: %v", k, i, err)
			}
		}
		c.setField(k, json.RawMessage("99"))
		b, _ := json.Marshal(c)
		var m map[string]any
		json.Unmarshal(b, &m)
		if int(m[k].(float64)) != n-1 {
			t.Fatalf("%s not clamped: %v", k, m[k])
		}
	}
	for k, v := range map[string]string{"HaloColorA": `"#12AB9F"`, "HaloColorB": `"#000000"`, "DiscColor": `"#FF0000"`} {
		if err := c.setField(k, json.RawMessage(v)); err != nil {
			t.Fatal(k, err)
		}
	}
	c.setField("HaloColorA", json.RawMessage(`"red"`))
	c.setField("SpinRPM", json.RawMessage(`500`))
	c.setField("MotionAmount", json.RawMessage(`-3`))
	c.setField("HaloBackdrop", json.RawMessage(`7`))
	if c.HaloColorA != "#7B5CFF" || c.SpinRPM != 78 || c.MotionAmount != 0 || c.HaloBackdrop != 1 || c.HaloColorB != "#000000" {
		t.Fatalf("clamps: %v %v %v %v", c.HaloColorA, c.SpinRPM, c.MotionAmount, c.HaloBackdrop)
	}
	if len(visPalettes) < 3 || visPalettes[len(visPalettes)-1].Name != "Custom" || visPalettes[0].Name != "Lyrics theme" {
		t.Fatal("palette order: first = lyrics theme, last = custom")
	}
	// widget opacity: 0 means 100 %, anything else 0.15..1
	c.Widgets["vinyl"].Opacity = 0.01
	c.Widgets["halo"].Opacity = 3
	c.validateWidgets()
	if c.Widgets["vinyl"].Opacity != 0.15 || c.Widgets["halo"].Opacity != 1 || c.Widgets["lyrics"].Opacity != 0 {
		t.Fatal("opacity clamp")
	}
}

func TestSmallWords(t *testing.T) {
	for _, w := range []string{"the", "I", "in", "my", "ve", "bir", "on,"} {
		if !isSmallWord(w) {
			t.Errorf("%q should be small", w)
		}
	}
	for _, w := range []string{"WORLD", "hands", "dünya", "Swimming"} {
		if isSmallWord(w) {
			t.Errorf("%q should be big", w)
		}
	}
}

func TestArrangeMath(t *testing.T) {
	// corners: 22 px handles on a 300×200 box
	cases := []struct{ x, y, want int }{{5, 5, 1}, {290, 10, 2}, {3, 195, 3}, {299, 199, 4}, {150, 100, 0}, {21, 100, 0}, {150, 21, 0}, {22, 22, 0}}
	for _, c := range cases {
		if got := cornerAt(c.x, c.y, 300, 200, 22); got != c.want {
			t.Errorf("cornerAt(%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}
	// wheel: one notch up = +7 %, down = −7 %, clamped
	if s := wheelScale(1, 120); math.Abs(s-1.07) > 1e-9 {
		t.Fatal(s)
	}
	if s := wheelScale(1, -120); math.Abs(s-1/1.07) > 1e-9 {
		t.Fatal(s)
	}
	if wheelScale(2.95, 1200) != maxScale || wheelScale(0.31, -1200) != minScale {
		t.Fatal("wheel clamp")
	}
	// pulling a corner twice as far from the centre doubles the size
	if s := dragScale(1, 100, 200); s != 2 {
		t.Fatal(s)
	}
	if s := dragScale(1.5, 100, 50); s != 0.75 {
		t.Fatal(s)
	}
	if dragScale(1, 0, 500) != maxScale || dragScale(1, 100, 0) != minScale {
		t.Fatal("drag clamp")
	}
	// centre ↔ top-left round trip
	x, y := topLeftFor(500, 300, 201, 100)
	if x != 400 || y != 250 {
		t.Fatalf("topLeft %d,%d", x, y)
	}
	if clampScale(math.NaN()) != 1 || clampScale(-3) != 1 {
		t.Fatal("bad scale input")
	}
	for _, c := range []string{"Progman", "WorkerW", "SHELLDLL_DefView"} {
		if !isDesktopClass(c) {
			t.Error(c)
		}
	}
	if isDesktopClass("Chrome_WidgetWin_1") || isDesktopClass("") {
		t.Error("app window treated as desktop")
	}
}

func TestMigrateV3Theme(t *testing.T) {
	// a v3 user on "Custom" (index 7 then) must stay on Custom, which is now the last theme
	os.WriteFile(configPath(), []byte(`{"Version":3,"Theme":7,"TextColor":"#112233"}`), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.Theme != len(themes)-1 || cfg.TextColor != "#112233" || cfg.Version != configVersion {
		t.Fatalf("custom theme lost: theme=%d %+v", cfg.Theme, cfg)
	}
	os.WriteFile(configPath(), []byte(`{"Version":3,"Theme":4}`), 0o644)
	cfg = defaultConfig()
	loadConfig()
	if cfg.Theme != 4 {
		t.Fatal("other themes must keep their index")
	}
	os.WriteFile(configPath(), []byte(`{"Version":4,"Theme":7}`), 0o644) // v4 index 7 = Black hole
	cfg = defaultConfig()
	loadConfig()
	if cfg.Theme != 7 || themes[7].Name != "Black hole" {
		t.Fatal("v4 files must not be migrated again")
	}
}

func TestNewStylesEffectsThemes(t *testing.T) {
	if len(styleNames) != 29 || styleNames[stSpotlight] != "Spotlight" || styleNames[stGlass] != "Glass" || styleNames[stScatter] != "Scatter" {
		t.Fatal("style constants out of sync with names")
	}
	if len(effectNames) != 12 || effectNames[fxGlass] != "Glass" || effectNames[fxChromatic] != "Chromatic" {
		t.Fatal("effect constants out of sync with names")
	}
	if themes[len(themes)-1].Name != "Custom" || len(themes) != 14 {
		t.Fatal("Custom must stay the last theme")
	}
	c := defaultConfig()
	for i := range styleNames {
		if err := c.setField("LyricsStyle", json.RawMessage(strconv.Itoa(i))); err != nil || c.LyricsStyle != i {
			t.Fatalf("style %d rejected", i)
		}
	}
	for i := range effectNames {
		if err := c.setField("Effect", json.RawMessage(strconv.Itoa(i))); err != nil || c.Effect != i {
			t.Fatalf("effect %d rejected", i)
		}
	}
	for i := range themes {
		if err := c.setField("Theme", json.RawMessage(strconv.Itoa(i))); err != nil || c.Theme != i {
			t.Fatalf("theme %d rejected", i)
		}
	}
	c.LyricsStyle, c.Effect, c.Theme = 99, 99, 99
	c.validateFields()
	if c.LyricsStyle >= len(styleNames) || c.Effect >= len(effectNames) || c.Theme >= len(themes) {
		t.Fatal("out of range values must be clamped")
	}
}

func TestStyleHelpers(t *testing.T) {
	if easeOutBounce(0) != 0 || math.Abs(easeOutBounce(1)-1) > 1e-9 {
		t.Fatal("bounce ends")
	}
	for x := 0.0; x <= 1; x += 0.01 {
		if v := easeOutBounce(x); v < 0 || v > 1.0001 {
			t.Fatalf("bounce(%v)=%v", x, v)
		}
	}
	L := &Line{T: 10, Sing: 3, Words: []Word{{"a", 10}, {"b", 10.5}, {"c", 10.52}}}
	if wordDur(L, 0) != 0.5 || wordDur(L, 1) != 0.15 || math.Abs(wordDur(L, 2)-2.48) > 1e-9 || wordDur(L, -1) != 1 || wordDur(L, 9) != 1 {
		t.Fatal("wordDur", wordDur(L, 0), wordDur(L, 1), wordDur(L, 2))
	}
	for _, w := range []string{"HELLO", "don't,", "çğış", "", "日本語"} {
		rs := []rune(w)
		for k := 0; k <= len(rs)+2; k++ {
			out := []rune(scramble(rs, k, 42))
			if len(out) != len(rs) {
				t.Fatalf("scramble changed length %q", w)
			}
			for j := 0; j < k && j < len(rs); j++ {
				if out[j] != rs[j] {
					t.Fatalf("scramble must keep resolved runes %q k=%d", w, k)
				}
			}
		}
		if scramble(rs, 0, 7) != scramble(rs, 0, 7) {
			t.Fatal("scramble must be deterministic")
		}
	}
	if lum(0xFFFFFFFF) < 0.99 || lum(0xFF000000) > 0.01 || lum(0xFFE8B04A) < 0.45 {
		t.Fatal("lum")
	}
}
