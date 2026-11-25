package main

// Languages: English is the source text; Turkish and Russian come from
// i18n/tr.json and i18n/ru.json. T(s) returns s in the chosen language, or s
// itself when there is no translation. A key holding "{n}" is a pattern; its
// value may list plural forms "one|few|many" (Russian) that Tn picks from.

import (
	"embed"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
)

//go:embed i18n/tr.json i18n/ru.json
var i18nFS embed.FS

var langNames = []string{"English", "Türkçe", "Русский"}
var langCodes = []string{"en", "tr", "ru"}

type langPack struct {
	code string
	dict map[string]string
	raw  []byte // the JSON as embedded, served to the settings page
}

var curPack atomic.Pointer[langPack]

// systemLang is the Windows display language ("en", "tr", "ru"); set by the
// Windows build.
var systemLang = func() string { return "en" }

func validLang(s string) bool { return s == "" || s == "en" || s == "tr" || s == "ru" }

// resolveLang turns the setting into a language code.
func resolveLang(setting string) string {
	if setting == "" {
		setting = systemLang()
	}
	if setting != "tr" && setting != "ru" {
		return "en"
	}
	return setting
}

// setLang switches the language (cheap when it doesn't change).
func setLang(setting string) {
	code := resolveLang(setting)
	if p := curPack.Load(); p != nil && p.code == code {
		return
	}
	p := &langPack{code: code}
	if code != "en" {
		b, err := i18nFS.ReadFile("i18n/" + code + ".json")
		if err == nil {
			var d map[string]string
			if json.Unmarshal(b, &d) == nil {
				p.dict, p.raw = d, b
			}
		}
	}
	if p.raw == nil {
		p.raw = []byte("{}")
	}
	curPack.Store(p)
	onLangChange()
}

// onLangChange is set by the Windows build to redraw what holds text.
var onLangChange = func() {}

func langCode() string {
	if p := curPack.Load(); p != nil {
		return p.code
	}
	return "en"
}

// T translates s.
func T(s string) string {
	p := curPack.Load()
	if p == nil || p.dict == nil {
		return s
	}
	if v, ok := p.dict[s]; ok && v != "" {
		return v
	}
	return s
}

// Tn fills the "{n}" pattern with n, picking the plural form.
func Tn(pattern string, n int) string {
	v := T(pattern)
	if strings.Contains(v, "|") {
		v = pluralPick(v, n, langCode())
	}
	return strings.ReplaceAll(v, "{n}", strconv.Itoa(n))
}

// pluralPick: "one|few|many" for Russian, "one|other" otherwise.
func pluralPick(v string, n int, code string) string {
	f := strings.Split(v, "|")
	if n < 0 {
		n = -n
	}
	i := 0
	if code == "ru" {
		switch {
		case n%10 == 1 && n%100 != 11:
			i = 0
		case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
			i = 1
		default:
			i = 2
		}
	} else if n != 1 {
		i = 1
	}
	if i >= len(f) {
		i = len(f) - 1
	}
	return f[i]
}

// langLocale is the Windows locale name for dates.
func langLocale() string {
	switch langCode() {
	case "tr":
		return "tr-TR"
	case "ru":
		return "ru-RU"
	}
	return "en-US"
}

// langJSON is the current dictionary for the settings page.
func langJSON() []byte {
	if p := curPack.Load(); p != nil {
		return p.raw
	}
	return []byte("{}")
}

// tStyle names a lyrics effect ("Orbit" the effect is not Orbit the app).
func tStyle(n string) string {
	if n == "Orbit" {
		if v := T("Orbit·style"); v != "Orbit·style" {
			return v
		}
		return n
	}
	return T(n)
}

// tJoin translates each " · " part of a message on its own.
func tJoin(s string) string {
	if p := curPack.Load(); p == nil || p.dict == nil {
		return s
	}
	if v := T(s); v != s {
		return v
	}
	parts := strings.Split(s, " · ")
	for i, p := range parts {
		parts[i] = tStyle(p)
	}
	return strings.Join(parts, " · ")
}

// upperLang: capitals the way the language writes them (Turkish i → İ).
func upperLang(s string) string {
	if langCode() == "tr" {
		return strings.ToUpperSpecial(unicode.TurkishCase, s)
	}
	return strings.ToUpper(s)
}

// upperFirst capitalises the first letter ("среда" → "Среда").
func upperFirst(s string) string {
	for i, r := range s {
		return upperLang(string(r)) + s[i+len(string(r)):]
	}
	return s
}
