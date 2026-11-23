package main

// Weather (Open-Meteo: free, no key) and clock helpers. Portable — no Windows calls here.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	weatherBase = "https://api.open-meteo.com"           // tests point these at a local server
	geoBase     = "https://geocoding-api.open-meteo.com" //
	httpWeather = &http.Client{Timeout: 12 * time.Second}
)

// Place is one search result.
type Place struct {
	Name    string  `json:"name"`
	Admin   string  `json:"admin"`   // state / province
	Country string  `json:"country"` //
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// Label: "Konya, Türkiye" (or "Springfield, Illinois, United States" when the name is ambiguous).
func (p Place) Label() string {
	parts := []string{p.Name}
	if p.Admin != "" && p.Admin != p.Name {
		parts = append(parts, p.Admin)
	}
	if p.Country != "" {
		parts = append(parts, p.Country)
	}
	return strings.Join(parts, ", ")
}

// geocode finds places by name ("konya", "new york", "paris, france").
func geocode(q string) ([]Place, error) {
	q = strings.TrimSpace(q)
	country := ""
	if i := strings.LastIndex(q, ","); i > 0 { // "paris, france" → filter by country afterwards
		country = strings.ToLower(strings.TrimSpace(q[i+1:]))
		q = strings.TrimSpace(q[:i])
	}
	if len([]rune(q)) < 2 {
		return nil, nil
	}
	u := fmt.Sprintf("%s/v1/search?name=%s&count=10&language=en&format=json", geoBase, url.QueryEscape(q))
	resp, err := httpWeather.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("geocoding: HTTP %d", resp.StatusCode)
	}
	var r struct {
		Results []struct {
			Name        string  `json:"name"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Admin1      string  `json:"admin1"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	out := []Place{}
	for _, x := range r.Results {
		if country != "" && !strings.HasPrefix(strings.ToLower(x.Country), country) && strings.ToLower(x.CountryCode) != country {
			continue
		}
		out = append(out, Place{Name: x.Name, Admin: x.Admin1, Country: x.Country, Lat: x.Latitude, Lon: x.Longitude})
	}
	return out, nil
}

// Weather is what the widget shows.
type Weather struct {
	OK       bool         `json:"ok"`
	Place    string       `json:"place"`
	Temp     float64      `json:"temp"`
	Feels    float64      `json:"feels"`
	Humidity float64      `json:"humidity"`
	Wind     float64      `json:"wind"` // km/h (mph with °F)
	Code     int          `json:"code"`
	IsDay    bool         `json:"isDay"`
	Kind     int          `json:"kind"`
	Text     string       `json:"text"`
	Unit     string       `json:"unit"` // "C" / "F"
	Days     []WeatherDay `json:"days"`
	Updated  time.Time    `json:"updated"`
	Err      string       `json:"err,omitempty"`
}

type WeatherDay struct {
	Date string  `json:"date"` // 2026-11-23
	Code int     `json:"code"`
	Kind int     `json:"kind"`
	Max  float64 `json:"max"`
	Min  float64 `json:"min"`
}

// what the icon draws
const (
	wkClearDay = iota
	wkClearNight
	wkPartlyDay
	wkPartlyNight
	wkCloudy
	wkFog
	wkDrizzle
	wkRain
	wkSnow
	wkStorm
)

var weatherKindNames = []string{"clear-day", "clear-night", "partly-day", "partly-night", "cloudy", "fog", "drizzle", "rain", "snow", "storm"}

// weatherKind maps WMO weather codes (Open-Meteo) to an icon and a short description.
func weatherKind(code int, day bool) (int, string) {
	clear, partly := wkClearDay, wkPartlyDay
	if !day {
		clear, partly = wkClearNight, wkPartlyNight
	}
	switch {
	case code == 0:
		return clear, "Clear"
	case code == 1:
		return clear, "Mostly clear"
	case code == 2:
		return partly, "Partly cloudy"
	case code == 3:
		return wkCloudy, "Cloudy"
	case code == 45 || code == 48:
		return wkFog, "Fog"
	case code >= 51 && code <= 55:
		return wkDrizzle, "Drizzle"
	case code == 56 || code == 57:
		return wkDrizzle, "Freezing drizzle"
	case code == 61:
		return wkRain, "Light rain"
	case code == 63:
		return wkRain, "Rain"
	case code == 65:
		return wkRain, "Heavy rain"
	case code == 66 || code == 67:
		return wkRain, "Freezing rain"
	case code >= 71 && code <= 75:
		return wkSnow, []string{"Light snow", "Snow", "Heavy snow"}[(code-71)/2]
	case code == 77:
		return wkSnow, "Snow grains"
	case code >= 80 && code <= 82:
		return wkRain, "Showers"
	case code == 85 || code == 86:
		return wkSnow, "Snow showers"
	case code == 95:
		return wkStorm, "Thunderstorm"
	case code == 96 || code == 99:
		return wkStorm, "Thunderstorm, hail"
	}
	return wkCloudy, "Cloudy"
}

var weather = struct {
	sync.Mutex
	w    Weather
	key  string // place+unit it was fetched for
	kick chan struct{}
}{kick: make(chan struct{}, 1)}

func weatherSnapshot() Weather {
	weather.Lock()
	defer weather.Unlock()
	w := weather.w
	w.Days = append([]WeatherDay(nil), w.Days...)
	return w
}

// kickWeather asks the weather loop to fetch now (new place, new unit, "Refresh").
func kickWeather() {
	select {
	case weather.kick <- struct{}{}:
	default:
	}
}

// fetchWeather downloads the current weather for a place.
func fetchWeather(place string, lat, lon float64, fahrenheit bool) (Weather, error) {
	unit, extra := "C", ""
	if fahrenheit {
		unit, extra = "F", "&temperature_unit=fahrenheit&wind_speed_unit=mph"
	}
	u := fmt.Sprintf("%s/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,apparent_temperature,relative_humidity_2m,is_day,weather_code,wind_speed_10m"+
		"&daily=weather_code,temperature_2m_max,temperature_2m_min&timezone=auto&forecast_days=5%s", weatherBase, lat, lon, extra)
	resp, err := httpWeather.Get(u)
	if err != nil {
		return Weather{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Weather{}, fmt.Errorf("weather: HTTP %d", resp.StatusCode)
	}
	var r struct {
		Current struct {
			Temp     float64 `json:"temperature_2m"`
			Feels    float64 `json:"apparent_temperature"`
			Humidity float64 `json:"relative_humidity_2m"`
			IsDay    int     `json:"is_day"`
			Code     int     `json:"weather_code"`
			Wind     float64 `json:"wind_speed_10m"`
		} `json:"current"`
		Daily struct {
			Time []string  `json:"time"`
			Code []int     `json:"weather_code"`
			Max  []float64 `json:"temperature_2m_max"`
			Min  []float64 `json:"temperature_2m_min"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Weather{}, err
	}
	c := r.Current
	kind, text := weatherKind(c.Code, c.IsDay == 1)
	w := Weather{OK: true, Place: place, Temp: c.Temp, Feels: c.Feels, Humidity: c.Humidity, Wind: c.Wind, Code: c.Code,
		IsDay: c.IsDay == 1, Kind: kind, Text: text, Unit: unit, Updated: time.Now()}
	d := r.Daily
	for i := range d.Time {
		if i >= len(d.Code) || i >= len(d.Max) || i >= len(d.Min) {
			break
		}
		k, _ := weatherKind(d.Code[i], true)
		w.Days = append(w.Days, WeatherDay{Date: d.Time[i], Code: d.Code[i], Kind: k, Max: d.Max[i], Min: d.Min[i]})
	}
	return w, nil
}

// weatherLoop keeps the weather fresh: every 15 minutes, right away when the place changes,
// and a minute later after a failure. onChange runs after every update (UI redraw).
func weatherLoop(get func() (place string, lat, lon float64, f, want bool), onChange func()) {
	var last time.Time
	wait := time.Duration(0)
	for {
		select {
		case <-weather.kick:
		case <-time.After(wait):
		}
		place, lat, lon, f, want := get()
		wait = 15 * time.Minute
		if !want || place == "" {
			wait = time.Minute
			continue
		}
		key := fmt.Sprintf("%s|%.3f|%.3f|%v", place, lat, lon, f)
		weather.Lock()
		same := weather.key == key && weather.w.OK
		weather.Unlock()
		if same && time.Since(last) < 2*time.Minute { // repeated kicks: don't hammer the API
			continue
		}
		w, err := fetchWeather(place, lat, lon, f)
		weather.Lock()
		if err != nil {
			logf("weather: %v", err)
			weather.w.Err = "Can't reach the weather service"
			if weather.key != key { // a different place: don't show the old place's weather
				weather.w = Weather{Place: place, Err: weather.w.Err}
			}
			wait = time.Minute
		} else {
			weather.w, weather.key = w, key
			last = time.Now()
		}
		weather.Unlock()
		onChange()
	}
}

/* ---------- clock ---------- */

var numberWords = []string{"twelve", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve",
	"thirteen", "fourteen", "quarter", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"}

func minuteWords(m int) string {
	switch {
	case m == 15:
		return "quarter"
	case m == 30:
		return "half"
	case m <= 20:
		return numberWords[m]
	case m == 25:
		return "twenty-five"
	}
	return "twenty-" + numberWords[m-20]
}

// clockWords: "it's" + top line + the hour in big, e.g. ("twenty-five to", "twelve"), ("", "ten o'clock").
// Rounded to five minutes like a person would say it.
func clockWords(h, m int) (string, string) {
	m = (m + 2) / 5 * 5
	if m == 60 {
		m, h = 0, h+1
	}
	hour := func(h int) string { return numberWords[(h%12+12)%12] }
	switch {
	case m == 0:
		return "", hour(h) + " o'clock"
	case m <= 30:
		return minuteWords(m) + " past", hour(h)
	}
	return minuteWords(60-m) + " to", hour(h + 1)
}

// clockParts: big time text, the small am/pm suffix (12-hour) and the seconds (if wanted).
func clockParts(t time.Time, h24, secs bool) (main, ampm, sec string) {
	h := t.Hour()
	if !h24 {
		ampm = "AM"
		if h >= 12 {
			ampm = "PM"
		}
		h = (h+11)%12 + 1
		main = fmt.Sprintf("%d:%02d", h, t.Minute())
	} else {
		main = fmt.Sprintf("%02d:%02d", h, t.Minute())
	}
	if secs {
		sec = fmt.Sprintf("%02d", t.Second())
	}
	return
}

// englishDate is the fallback when Windows can't format the date in the user's language.
func englishDate(t time.Time, long bool) string {
	if long {
		return t.Format("Monday, 2 January")
	}
	return t.Format("Mon, 2 Jan")
}

// fontFor picks the installed font for a font group and weight. Families that come in weights
// (Segoe UI, Bahnschrift, Cascadia) use the real weight; others fall back to bold at ≥ 600.
func fontFor(group, weight int, installed []string) (string, bool) {
	has := map[string]bool{}
	for _, f := range installed {
		has[f] = true
	}
	type opt struct {
		name string
		min  int // lightest weight this face stands for
	}
	var chain [][]opt
	switch group {
	case 1: // condensed
		chain = [][]opt{{{"Bahnschrift Light Condensed", 0}, {"Bahnschrift SemiLight Condensed", 300}, {"Bahnschrift Condensed", 400}, {"Bahnschrift SemiBold Condensed", 600}, {"Bahnschrift Bold Condensed", 700}},
			{{"Bahnschrift Light", 0}, {"Bahnschrift", 400}, {"Bahnschrift SemiBold", 600}, {"Bahnschrift Bold", 700}}, {{"Arial Narrow", 0}}, {{"Impact", 0}}}
	case 2: // serif
		chain = [][]opt{{{"Georgia", 0}}, {{"Cambria", 0}}, {{"Times New Roman", 0}}}
	case 3: // mono
		chain = [][]opt{{{"Cascadia Mono Light", 0}, {"Cascadia Mono SemiLight", 300}, {"Cascadia Mono", 400}, {"Cascadia Mono SemiBold", 600}}, {{"Consolas", 0}}, {{"Courier New", 0}}}
	case 4: // handwritten
		chain = [][]opt{{{"Ink Free", 0}}, {{"Segoe Print", 0}}, {{"Segoe Script", 0}}}
	default: // modern
		chain = [][]opt{{{"Segoe UI Light", 0}, {"Segoe UI Semilight", 300}, {"Segoe UI", 400}, {"Segoe UI Semibold", 600}, {"Segoe UI Black", 850}},
			{{"Arial", 0}}}
	}
	for _, fam := range chain {
		best, multi := "", len(fam) > 1
		for _, o := range fam {
			if has[o.name] && (best == "" || weight >= o.min) {
				best = o.name
			}
		}
		if best != "" {
			bold := weight >= 600 && !multi
			if multi && weight >= 700 && !strings.Contains(best, "Bold") && !strings.Contains(best, "Black") && !strings.Contains(best, "SemiBold") && !strings.Contains(best, "Semibold") {
				bold = true // the family has no heavier face installed
			}
			return best, bold
		}
	}
	return "Segoe UI", weight >= 600
}

// clockBox: the clock's window size for a style (k = screen scale × widget size).
func clockBox(style int, date bool, k float64) (int, int) {
	w, h := 360.0, 150.0
	switch style {
	case ckStacked:
		w, h = 260, 240
	case ckMinimal:
		w, h = 380, 70
	case ckAnalog:
		w, h = 230, 230
	case ckWords:
		w, h = 380, 160
	}
	if date && style != ckMinimal {
		h += 34
	}
	return int(w * k), int(h * k)
}

// weatherBox: the weather widget's window size.
func weatherBox(style int, k float64) (int, int) {
	switch style {
	case 1: // minimal
		return int(230 * k), int(190 * k)
	case 2: // forecast
		return int(380 * k), int(250 * k)
	}
	return int(380 * k), int(160 * k)
}
