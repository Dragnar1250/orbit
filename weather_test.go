package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMeteo answers like Open-Meteo (geocoding + forecast).
var fakeMu sync.Mutex // guards the fake server's "down" flag and the test's place

func fakeMeteo(t testing.TB, code int, fail *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fakeMu.Lock()
		down := fail != nil && *fail
		fakeMu.Unlock()
		if down {
			http.Error(w, "down", 503)
			return
		}
		switch r.URL.Path {
		case "/v1/search":
			q := strings.ToLower(r.URL.Query().Get("name"))
			res := []map[string]any{}
			if strings.HasPrefix("konya", q) {
				res = append(res, map[string]any{"name": "Konya", "latitude": 37.87, "longitude": 32.48, "country": "Türkiye", "country_code": "TR", "admin1": "Konya"})
			}
			if strings.HasPrefix("paris", q) {
				res = append(res, map[string]any{"name": "Paris", "latitude": 48.85, "longitude": 2.35, "country": "France", "country_code": "FR", "admin1": "Île-de-France"},
					map[string]any{"name": "Paris", "latitude": 33.66, "longitude": -95.55, "country": "United States", "country_code": "US", "admin1": "Texas"})
			}
			json.NewEncoder(w).Encode(map[string]any{"results": res})
		case "/v1/forecast":
			f := r.URL.Query().Get("temperature_unit") == "fahrenheit"
			temp := 18.4
			if f {
				temp = 65.1
			}
			fmt.Fprintf(w, `{"current":{"temperature_2m":%v,"apparent_temperature":%v,"relative_humidity_2m":62,"is_day":1,"weather_code":%d,"wind_speed_10m":12.3},
			"daily":{"time":["2026-11-23","2026-11-24","2026-11-25","2026-11-26","2026-11-27"],"weather_code":[%d,61,3,71,95],
			"temperature_2m_max":[21,17,15,6,12],"temperature_2m_min":[12,9,8,-1,5]}}`, temp, temp-2, code, code)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGeocode(t *testing.T) {
	srv := fakeMeteo(t, 0, nil)
	defer srv.Close()
	geoBase = srv.URL
	p, err := geocode("Kon")
	if err != nil || len(p) != 1 || p[0].Label() != "Konya, Türkiye" || p[0].Lat != 37.87 {
		t.Fatalf("%v %+v", err, p)
	}
	p, _ = geocode("paris")
	if len(p) != 2 || p[0].Label() != "Paris, Île-de-France, France" {
		t.Fatalf("%+v", p)
	}
	p, _ = geocode("paris, united")
	if len(p) != 1 || p[0].Country != "United States" {
		t.Fatalf("country filter: %+v", p)
	}
	p, _ = geocode("Paris, FR") // country code works too
	if len(p) != 1 || p[0].Country != "France" {
		t.Fatalf("country code filter: %+v", p)
	}
	if p, err := geocode(" k "); err != nil || p != nil {
		t.Fatal("1 letter: no search")
	}
}

func TestFetchWeather(t *testing.T) {
	srv := fakeMeteo(t, 2, nil)
	defer srv.Close()
	weatherBase = srv.URL
	w, err := fetchWeather("Konya, Türkiye", 37.87, 32.48, false)
	if err != nil || !w.OK || w.Temp != 18.4 || w.Unit != "C" || w.Kind != wkPartlyDay || w.Text != "Partly cloudy" || len(w.Days) != 5 || w.Humidity != 62 {
		t.Fatalf("%v %+v", err, w)
	}
	if w.Days[3].Kind != wkSnow || w.Days[4].Kind != wkStorm || w.Days[3].Min != -1 {
		t.Fatalf("days %+v", w.Days)
	}
	w, _ = fetchWeather("X", 1, 2, true)
	if w.Temp != 65.1 || w.Unit != "F" {
		t.Fatalf("fahrenheit %+v", w)
	}
}

func TestWeatherKinds(t *testing.T) {
	cases := []struct {
		code int
		day  bool
		kind int
	}{{0, true, wkClearDay}, {0, false, wkClearNight}, {1, false, wkClearNight}, {2, true, wkPartlyDay}, {2, false, wkPartlyNight}, {3, true, wkCloudy},
		{45, true, wkFog}, {48, false, wkFog}, {51, true, wkDrizzle}, {57, true, wkDrizzle}, {61, true, wkRain}, {65, true, wkRain}, {67, true, wkRain},
		{71, true, wkSnow}, {75, true, wkSnow}, {77, true, wkSnow}, {80, true, wkRain}, {82, true, wkRain}, {85, true, wkSnow}, {95, true, wkStorm}, {99, false, wkStorm}, {12345, true, wkCloudy}}
	for _, c := range cases {
		k, txt := weatherKind(c.code, c.day)
		if k != c.kind || txt == "" {
			t.Errorf("code %d day=%v → %d %q, want %d", c.code, c.day, k, txt, c.kind)
		}
	}
	if len(weatherKindNames) != wkStorm+1 {
		t.Fatal("kind names")
	}
}

func TestWeatherLoop(t *testing.T) {
	fail := false
	srv := fakeMeteo(t, 63, &fail)
	defer srv.Close()
	weatherBase = srv.URL
	weather.Lock()
	weather.w, weather.key = Weather{}, ""
	weather.Unlock()
	place := "Konya, Türkiye"
	getPlace := func() string { fakeMu.Lock(); defer fakeMu.Unlock(); return place }
	changed := make(chan bool, 10)
	go weatherLoop(func() (string, float64, float64, bool, bool) { return getPlace(), 37.8, 32.4, false, true }, func() { changed <- true })
	select {
	case <-changed:
	case <-time.After(3 * time.Second):
		t.Fatal("no first fetch")
	}
	w := weatherSnapshot()
	if !w.OK || w.Kind != wkRain || w.Place != place {
		t.Fatalf("%+v", w)
	}
	// service down + a new place: no stale weather of the old place, an error to show
	fakeMu.Lock()
	fail = true
	place = "Paris, France"
	fakeMu.Unlock()
	kickWeather()
	select {
	case <-changed:
	case <-time.After(3 * time.Second):
		t.Fatal("no update after failure")
	}
	w = weatherSnapshot()
	if w.OK || w.Err == "" || w.Place != "Paris, France" {
		t.Fatalf("after failure: %+v", w)
	}
}

func TestClockWords(t *testing.T) {
	cases := map[[2]int][2]string{
		{11, 35}: {"twenty-five to", "twelve"}, {10, 0}: {"", "ten o'clock"}, {10, 2}: {"", "ten o'clock"}, {10, 58}: {"", "eleven o'clock"},
		{10, 15}: {"quarter past", "ten"}, {10, 30}: {"half past", "ten"}, {10, 45}: {"quarter to", "eleven"}, {23, 50}: {"ten to", "twelve"},
		{0, 5}: {"five past", "twelve"}, {12, 20}: {"twenty past", "twelve"}, {13, 25}: {"twenty-five past", "one"}, {13, 40}: {"twenty to", "two"},
		{23, 59}: {"", "twelve o'clock"}, {6, 9}: {"ten past", "six"}, {6, 53}: {"five to", "seven"},
	}
	for in, want := range cases {
		a, b := clockWords(in[0], in[1])
		if a != want[0] || b != want[1] {
			t.Errorf("%02d:%02d → %q %q, want %q %q", in[0], in[1], a, b, want[0], want[1])
		}
	}
	for h := 0; h < 24; h++ { // every minute of the day gives words
		for m := 0; m < 60; m++ {
			if _, b := clockWords(h, m); b == "" || strings.Contains(b, "fifteen") {
				t.Fatalf("%d:%d", h, m)
			}
		}
	}
}

func TestClockParts(t *testing.T) {
	tm := time.Date(2026, 11, 23, 23, 5, 9, 0, time.Local)
	if m, a, s := clockParts(tm, true, false); m != "23:05" || a != "" || s != "" {
		t.Fatal(m, a, s)
	}
	if m, a, s := clockParts(tm, false, true); m != "11:05" || a != "PM" || s != "09" {
		t.Fatal(m, a, s)
	}
	if m, a, _ := clockParts(time.Date(2026, 1, 1, 0, 7, 0, 0, time.Local), false, false); m != "12:07" || a != "AM" {
		t.Fatal(m, a)
	}
	if m, a, _ := clockParts(time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local), false, false); m != "12:00" || a != "PM" {
		t.Fatal(m, a)
	}
	if englishDate(tm, true) != "Monday, 23 November" || englishDate(tm, false) != "Mon, 23 Nov" {
		t.Fatal(englishDate(tm, true))
	}
}

func TestFontFor(t *testing.T) {
	win11 := []string{"Segoe UI", "Segoe UI Light", "Segoe UI Semilight", "Segoe UI Semibold", "Segoe UI Black", "Bahnschrift", "Bahnschrift Light Condensed",
		"Bahnschrift Condensed", "Bahnschrift SemiBold Condensed", "Bahnschrift Bold Condensed", "Georgia", "Consolas", "Cascadia Mono", "Cascadia Mono Light", "Ink Free", "Arial"}
	cases := []struct {
		g, w int
		f    string
		bold bool
	}{{0, 100, "Segoe UI Light", false}, {0, 300, "Segoe UI Semilight", false}, {0, 400, "Segoe UI", false}, {0, 600, "Segoe UI Semibold", false},
		{0, 900, "Segoe UI Black", false}, {1, 300, "Bahnschrift Light Condensed", false}, {1, 700, "Bahnschrift Bold Condensed", false},
		{2, 300, "Georgia", false}, {2, 700, "Georgia", true}, {3, 200, "Cascadia Mono Light", false}, {3, 800, "Cascadia Mono", true}, {4, 400, "Ink Free", false}}
	for _, c := range cases {
		f, b := fontFor(c.g, c.w, win11)
		if f != c.f || b != c.bold {
			t.Errorf("group %d weight %d → %q bold=%v, want %q %v", c.g, c.w, f, b, c.f, c.bold)
		}
	}
	// Windows 7-ish: only basics installed → sensible fallbacks
	old := []string{"Arial", "Georgia", "Courier New", "Segoe Print", "Impact"}
	for g, want := range []string{"Arial", "Impact", "Georgia", "Courier New", "Segoe Print"} {
		if f, _ := fontFor(g, 400, old); f != want {
			t.Errorf("old PC group %d → %q want %q", g, f, want)
		}
	}
	if f, _ := fontFor(0, 400, nil); f != "Segoe UI" {
		t.Fatal("nothing installed")
	}
}

func TestWidgetBoxes(t *testing.T) {
	for st := range clockStyleNames {
		w, h := clockBox(st, true, 1)
		w2, h2 := clockBox(st, false, 2)
		if w <= 0 || h <= 0 || w2 != 2*w || (st != ckMinimal && h2 >= 2*h) {
			t.Errorf("clock style %d: %d×%d / %d×%d", st, w, h, w2, h2)
		}
	}
	for st := range weatherStyleNames {
		if w, h := weatherBox(st, 1.5); w < 200 || h < 100 {
			t.Errorf("weather %d: %d×%d", st, w, h)
		}
	}
	if !validWidget("clock") || !validWidget("weather") || validWidget("glow") || validWidget("") || len(allWidgets) != 5 {
		t.Fatal("widget names")
	}
}

func TestV7Settings(t *testing.T) {
	c := defaultConfig()
	if c.ShowClock || c.ShowWeather || c.ShowGlow || c.ClockWeight != 300 || !c.WeatherAnimate {
		t.Fatal("defaults")
	}
	for k, n := range map[string]int{"ClockStyle": len(clockStyleNames), "ClockFont": len(fontGroupNames), "ClockColorMode": len(clockColorNames), "ClockAlign": len(alignNames),
		"WeatherStyle": len(weatherStyleNames), "GlowStyle": len(glowStyleNames), "GlowWhere": len(glowWhereNames), "GlowMode": len(glowModeNames),
		"GlowColorMode": len(glowColorNames), "GlowScreens": len(glowScreenNames)} {
		for i := 0; i < n; i++ {
			if err := c.setField(k, json.RawMessage(fmt.Sprint(i))); err != nil {
				t.Fatalf("%s=%d: %v", k, i, err)
			}
		}
		c.setField(k, json.RawMessage("77"))
		b, _ := json.Marshal(c)
		var m map[string]any
		json.Unmarshal(b, &m)
		if int(m[k].(float64)) != n-1 {
			t.Fatalf("%s not clamped: %v", k, m[k])
		}
	}
	c.setField("ClockWeight", json.RawMessage("449"))
	c.setField("ClockSpacing", json.RawMessage("9"))
	c.setField("GlowSpeed", json.RawMessage("0"))
	c.setField("GlowReach", json.RawMessage("99"))
	c.setField("ClockColor", json.RawMessage(`"blue"`))
	c.setField("WeatherLat", json.RawMessage("500"))
	if c.ClockWeight != 400 || c.ClockSpacing != 0.5 || c.GlowSpeed != 0.05 || c.GlowReach != 2.5 || c.ClockColor != "#FFFFFF" || c.WeatherLat != 90 {
		t.Fatalf("%v %v %v %v %v %v", c.ClockWeight, c.ClockSpacing, c.GlowSpeed, c.GlowReach, c.ClockColor, c.WeatherLat)
	}
	c.validateWidgets()
	for _, w := range allWidgets {
		if c.Widgets[w] == nil {
			t.Fatal("widget not pre-created: " + w)
		}
	}
}

func TestWeatherEndpoints(t *testing.T) {
	srv0 := fakeMeteo(t, 0, nil)
	defer srv0.Close()
	geoBase = srv0.URL
	cfg = defaultConfig()
	changed := ""
	hooks.SettingsChanged = func(k string) { changed = k }
	srv, err := startSettingsServer("")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", srv.port)
	tok := "?t=" + srv.token
	r, err := http.Get(base + "/api/geocode" + tok + "&q=konya")
	if err != nil || r.StatusCode != 200 {
		t.Fatal(err, r.StatusCode)
	}
	var res []map[string]any
	json.NewDecoder(r.Body).Decode(&res)
	r.Body.Close()
	if len(res) != 1 || res[0]["label"] != "Konya, Türkiye" {
		t.Fatalf("%v", res)
	}
	if r, _ := http.Get(base + "/api/geocode?q=konya"); r.StatusCode != 403 {
		t.Fatal("geocode needs the token")
	}
	if r, _ := http.Get(base + "/api/geocode" + tok + "&q=" + strings.Repeat("a", 200)); r.StatusCode != 400 {
		t.Fatal("long query")
	}
	geoBase = "http://127.0.0.1:1" // service down → 502 with a message
	if r, _ := http.Get(base + "/api/geocode" + tok + "&q=konya"); r.StatusCode != 502 {
		t.Fatal("down → 502")
	}
	post := func(body string) int {
		r, err := http.Post(base+"/api/action"+tok, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if c := post(`{"name":"weatherPlace","value":{"label":"Konya, Türkiye","lat":37.87,"lon":32.48}}`); c != 200 {
		t.Fatal(c)
	}
	for _, bad := range []string{`{"name":"weatherPlace","value":{"label":"","lat":1,"lon":1}}`, `{"name":"weatherPlace","value":{"label":"X","lat":91,"lon":1}}`,
		`{"name":"weatherPlace","value":{"label":"X","lat":1,"lon":-181}}`, `{"name":"weatherPlace","value":"x"}`, `{"name":"widget","value":{"name":"glow","x":1,"y":1,"scale":1}}`} {
		if c := post(bad); c != 400 {
			t.Fatalf("%s → %d", bad, c)
		}
	}
	if c := post(`{"name":"weatherRefresh"}`); c != 200 {
		t.Fatal("refresh")
	}
	drainUI()
	if cfg.WeatherCity != "Konya, Türkiye" || cfg.WeatherLat != 37.87 || changed != "WeatherCity" {
		t.Fatalf("%q %v %q", cfg.WeatherCity, cfg.WeatherLat, changed)
	}
	r, _ = http.Get(base + "/api/state" + tok)
	var st map[string]any
	json.NewDecoder(r.Body).Decode(&st)
	r.Body.Close()
	lists := st["lists"].(map[string]any)
	if len(lists["glowStyles"].([]any)) != 10 || len(st["all"].([]any)) != 5 || st["dateLong"] == "" || st["weather"] == nil {
		t.Fatalf("state %v", st["lists"])
	}
	srv.ln.Close()
}

func TestConstellationLayout(t *testing.T) {
	for _, n := range []int{0, 1, 2, 7, 30, 80} {
		for seed := int64(0); seed < 25; seed++ {
			W, H, m := 1200.0, 300.0, 40.0
			a := constellationLayout(n, seed, W, H, m)
			b := constellationLayout(n, seed, W, H, m)
			if len(a) != n {
				t.Fatal("count")
			}
			for i := range a {
				if a[i] != b[i] {
					t.Fatal("same seed must give the same sky")
				}
				p := a[i]
				if p.X < m-0.01 || p.X > W-m+0.01 || p.Y < m-0.01 || p.Y > H-m*1.6+0.01 {
					t.Fatalf("n=%d seed=%d star %d outside the box: %+v", n, seed, i, p)
				}
				if p.Link >= 0 && (p.Link >= i-1 || i%3 != 0) {
					t.Fatalf("bad extra link %d → %d", i, p.Link)
				}
				if i > 0 && math.Hypot(p.X-a[i-1].X, p.Y-a[i-1].Y) < 1 {
					t.Fatalf("star %d sits on the previous one", i)
				}
			}
		}
	}
	// a tiny box still works
	if len(constellationLayout(5, 1, 10, 10, 20)) != 5 {
		t.Fatal("tiny box")
	}
	// longer lines of a group keep the earlier stars where they were (layout of n+k starts with layout of n)
	a, b := constellationLayout(6, 9, 1000, 300, 30), constellationLayout(12, 9, 1000, 300, 30)
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("stars must not jump when more words come")
		}
	}
}

func TestStarShapes(t *testing.T) {
	for sh := 0; sh < len(starShapeNames)-1; sh++ {
		pts := starPoints(sh, 10)
		if sh == 3 || sh == 4 {
			if pts != nil {
				t.Fatal("orb / cross are drawn without a polygon")
			}
			continue
		}
		if len(pts) < 4 {
			t.Fatalf("shape %d: %d points", sh, len(pts))
		}
		for _, p := range pts {
			if math.Hypot(p[0], p[1]) > 10.5 {
				t.Fatalf("shape %d point outside radius: %v", sh, p)
			}
		}
	}
	if len(starPoints(0, 10)) != 8 || len(starPoints(1, 10)) != 10 {
		t.Fatal("sparkle has 4 spikes, star 5")
	}
	seen := map[int]bool{}
	for i := 0; i < 12; i++ {
		s := shapeFor(6, i)
		if s < 0 || s > 5 {
			t.Fatal("mixed shape out of range")
		}
		seen[s] = true
		if shapeFor(2, i) != 2 {
			t.Fatal("fixed shape")
		}
	}
	if len(seen) != 6 {
		t.Fatal("Mixed should use every symbol", seen)
	}
}

func TestBallHop(t *testing.T) {
	x, y := ballHop(0, 100, 200, 100, 0, 50)
	if x != 0 || y != 100 {
		t.Fatal("start")
	}
	x, y = ballHop(0, 100, 200, 100, 1, 50)
	if x != 200 || y != 100 {
		t.Fatal("lands on the next word")
	}
	x, y = ballHop(0, 100, 200, 100, 0.5, 50)
	if x != 100 || y != 50 {
		t.Fatal("peak", x, y)
	}
	if _, y := ballHop(0, 0, 10, 0, 7, 50); y != 0 {
		t.Fatal("clamped")
	}
	if styleNames[stConstellation] != "Constellation" || styleNames[stBouncing] != "Bouncing" || len(styleNames) != 29 {
		t.Fatal("style names")
	}
}

func TestAmbientAndCorners(t *testing.T) {
	for eff := range ambEffectNames {
		lo, hi := 9.0, -9.0
		for i := 0; i < 2000; i++ {
			v := ambLevel(eff, float64(i)*0.01, float64(i%10)/10)
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		if lo < 0.2 || hi > 1.0001 {
			t.Errorf("effect %d range %.2f..%.2f (never dark, never over full)", eff, lo, hi)
		}
		if (eff == 0 || eff == 4) && lo != 1 {
			t.Errorf("effect %d should be steady", eff)
		}
		if (eff == 1 || eff == 3) && hi-lo < 0.1 {
			t.Errorf("effect %d should move", eff)
		}
	}
	if ambLevel(2, 0, 0) >= ambLevel(2, 0, 1) {
		t.Fatal("beat brightens with the bass")
	}
	// corners: 0 bottom-left, 1 bottom-right, 2 top-left, 3 top-right
	for _, c := range []struct {
		x, y float64
		want int
	}{{10, 1000, 0}, {1900, 1000, 1}, {10, 10, 2}, {1900, 10, 3}, {959, 541, 0}, {961, 539, 3}} {
		if got := cornerFor(c.x, c.y, 0, 0, 1920, 1080); got != c.want {
			t.Errorf("cornerFor(%v,%v) = %d want %d", c.x, c.y, got, c.want)
		}
	}
	if len(cornerNames) != 4 {
		t.Fatal("corner names")
	}
	// toolbar: in the top-right corner of the visible part, never starting left of it, evenly spaced
	xs := toolbarLayout(5, 0, 400, 28, 6)
	if math.Abs(xs[4]+28-392) > 0.01 || xs[1]-xs[0] != 34 {
		t.Fatal("top-right", xs)
	}
	xs = toolbarLayout(5, 300, 360, 28, 6) // narrow visible part: start at its left edge
	if xs[0] < 302-0.01 {
		t.Fatal("left edge", xs)
	}
	c := defaultConfig()
	if !c.HoverEdit || !c.AmbVinyl || !c.AmbHalo || c.VinylLayout != 0 || c.AmbBright <= 0 || c.AmbBright > 0.5 {
		t.Fatal("defaults: hover on, gentle ambient light, free layout")
	}
	c.setField("AmbSize", json.RawMessage("9"))
	c.setField("AmbBright", json.RawMessage("-1"))
	c.setField("AmbEffect", json.RawMessage("99"))
	c.setField("VinylLayout", json.RawMessage("5"))
	c.setField("AmbColor", json.RawMessage(`"x"`))
	if c.AmbSize != 2 || c.AmbBright != 0 || c.AmbEffect != len(ambEffectNames)-1 || c.VinylLayout != 1 || c.AmbColor != "#FF8A3D" {
		t.Fatalf("clamps %+v", []any{c.AmbSize, c.AmbBright, c.AmbEffect, c.VinylLayout, c.AmbColor})
	}
}

func TestToolbars(t *testing.T) {
	ids := func(name string) string {
		var s []string
		for _, b := range toolbarFor(name) {
			s = append(s, b.id)
		}
		return strings.Join(s, ",")
	}
	want := map[string]string{"lyrics": "settings,hide", "vinyl": "next,themes,colors,settings,hide", "halo": "next,themes,colors,settings,hide",
		"clock": "next,settings,hide", "weather": "next,settings,hide"}
	for n, w := range want {
		if ids(n) != w {
			t.Errorf("%s: %s want %s", n, ids(n), w)
		}
	}
	if toolbarFor("halo")[2].label != "Color" || toolbarFor("vinyl")[2].label != "Colors" || toolbarFor("lyrics")[1].label != "Hide it" {
		t.Fatal("labels")
	}
	seen := map[string]bool{}
	c := "nonsense"
	for i := 0; i < len(colorPresets); i++ {
		c = nextPreset(c)
		seen[c] = true
	}
	if len(seen) != len(colorPresets) || nextPreset(colorPresets[len(colorPresets)-1]) != colorPresets[0] || nextPreset("#ff4d6d") != colorPresets[1] {
		t.Fatal("presets cycle through every colour", seen)
	}
}

func TestHiResCover(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		switch {
		case r.URL.Path == "/search":
			host := "http://" + r.Host
			fmt.Fprintf(w, `{"results":[{"artworkUrl100":"%s/img/100x100bb.jpg"}]}`, host)
		case strings.HasPrefix(r.URL.Path, "/img/"):
			w.Write([]byte("IMG:" + r.URL.Path))
		}
	}))
	defer srv.Close()
	itunesBase = srv.URL
	if b := string(fetchCoverHiRes("Swim (Official)", "Chase Atlantic, X")); b != "IMG:/img/2000x2000bb.jpg" {
		t.Fatal("hi-res:", b)
	}
	if b := string(fetchCoverOnline("Swim", "Chase Atlantic")); b != "IMG:/img/600x600bb.jpg" {
		t.Fatal("normal:", b)
	}
	c := defaultConfig()
	if c.FsMode != fsmArt || c.FsBg != fsbBlackHole || !c.FsLyrics || c.FsFPS != 30 || c.FsLyricsStyle != stGlass {
		t.Fatal("full screen defaults")
	}
	for k, v := range map[string]string{"FsMode": "9", "FsBg": "9", "FsVis": "99", "FsFPS": "45", "FsTextSize": "999", "FsFont": `""`, "FsLyricsStyle": "999",
		"FsFrameBright": "5", "FsTextColor": `"x"`, "FsLyricsWidth": "5"} {
		c.setField(k, json.RawMessage(v))
	}
	if c.FsMode != 3 || c.FsBg != len(fsBgNames)-1 || c.FsVis != len(visNames) || c.FsFPS != 30 || c.FsTextSize != 200 || c.FsFont != "Segoe UI" ||
		c.FsLyricsStyle != len(styleNames)-1 || c.FsFrameBright != 1 || c.FsTextColor != "#FFFFFF" || c.FsLyricsWidth != 30 {
		t.Fatalf("clamps %+v", c)
	}
	c.setField("FsFPS", json.RawMessage("60"))
	if c.FsFPS != 60 {
		t.Fatal("60 fps allowed")
	}
}
