package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLangPacks(t *testing.T) {
	defer setLang("en")
	for _, code := range []string{"tr", "ru"} {
		b, err := i18nFS.ReadFile("i18n/" + code + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var d map[string]string
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatal(err)
		}
		for k, v := range d {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: empty for %q", code, k)
			}
			if strings.Count(k, "%") != strings.Count(v, "%") && !strings.Contains(k, "%%") {
				t.Errorf("%s: %% differ: %q → %q", code, k, v)
			}
			for _, ph := range []string{"{x}", "{v}", "{old}"} {
				if strings.Contains(k, ph) != strings.Contains(v, ph) {
					t.Errorf("%s: %s lost: %q → %q", code, ph, k, v)
				}
			}
		}
		// every name the app shows has a translation
		lists := [][]string{styleNames, effectNames, cornerNames, discNames, clockStyleNames, clockColorNames, alignNames, weatherStyleNames,
			glowStyleNames, glowWhereNames, glowModeNames, glowColorNames, glowScreenNames, visNames, visCardNames, motionNames, caseNames,
			fsScreenNames, uiThemeNames, fsBgNames, fsCoverNames, fsArmNames, vinylLayoutNames, swapStyleNames, ambColorNames, ambEffectNames, discColorNames, starShapeNames}
		for _, l := range lists {
			for _, n := range l {
				if n == "Orbit" {
					n = "Orbit·style"
				}
				if _, ok := d[n]; !ok {
					t.Errorf("%s: no translation for %q", code, n)
				}
			}
		}
		for _, th := range themes {
			if _, ok := d[th.Name]; !ok {
				t.Errorf("%s: no theme %q", code, th.Name)
			}
		}
		for _, c := range constellations {
			if _, ok := d[c.Name]; !ok {
				t.Errorf("%s: no sky %q", code, c.Name)
			}
		}
	}
	setLang("ru")
	for n, want := range map[int]string{1: "1 песня", 3: "3 песни", 5: "5 песен", 11: "11 песен", 21: "21 песня", 22: "22 песни"} {
		if got := Tn("{n} songs", n); got != want {
			t.Errorf("ru plural %d: %q want %q", n, got, want)
		}
	}
	if tStyle("Orbit") != "Орбита" || T("Constellation") != "Созвездие" {
		t.Error("ru names")
	}
	setLang("tr")
	if T("Constellation") != "Takımyıldız" || upperLang("istanbul") != "İSTANBUL" {
		t.Error("tr")
	}
	if got := tJoin("Lyrics · Scatter"); got != "Şarkı sözleri · Dağınık" {
		t.Errorf("tJoin %q", got)
	}
}

func TestClockWordsLang(t *testing.T) {
	cases := []struct {
		code string
		h, m int
		want string
	}{
		{"tr", 10, 5, "saat|onu|beş geçiyor"}, {"tr", 10, 15, "saat|onu|çeyrek geçiyor"}, {"tr", 10, 30, "saat|on|buçuk"},
		{"tr", 10, 40, "saat|on bire|yirmi var"}, {"tr", 22, 45, "saat|on bire|çeyrek var"}, {"tr", 0, 0, "saat|on iki|"}, {"tr", 6, 0, "saat|altı|"},
		{"ru", 10, 5, "сейчас|пять минут|одиннадцатого"}, {"ru", 12, 30, "сейчас|половина|первого"}, {"ru", 10, 45, "сейчас|без четверти|одиннадцать"},
		{"ru", 0, 55, "сейчас|без пяти|час"}, {"ru", 15, 0, "сейчас|три|часа"}, {"ru", 17, 0, "сейчас|пять|часов"}, {"ru", 13, 0, "сейчас|час|"},
		{"en", 10, 5, "it's|five past|ten"},
	}
	for _, c := range cases {
		its, top, hour, after := clockWordsIn(c.code, c.h, c.m)
		got := its + "|" + strings.TrimSpace(top+" "+"") + "|" + hour
		if c.code == "tr" || (c.code == "ru" && c.m == 0) {
			got = its + "|" + hour + "|" + after
		}
		if got != c.want {
			t.Errorf("%s %d:%02d = %q want %q", c.code, c.h, c.m, got, c.want)
		}
	}
}
