package main

import "testing"

func TestMacNumber(t *testing.T) {
	for in, want := range map[string]float64{"12.5": 12.5, "12,5": 12.5, "1.234,5": 1234.5, "1,234.5": 1234.5, "215000": 215000, "3.0E+2": 300, "": 0, "abc": 0, "-5": 0, "NaN": 0} {
		if got := macNumber(in); got != want {
			t.Fatalf("%q: %v want %v", in, got, want)
		}
	}
}

func TestParseMacNowPlaying(t *testing.T) {
	n, ok := parseMacNowPlaying("spotify\tplaying\tSwim\tChase Atlantic\tPhases\t42,25\t240000\thttps://i.scdn.co/image/x\n")
	if !ok || n.Title != "Swim" || n.Pos != 42.25 || n.Dur != 240 || n.Art != "https://i.scdn.co/image/x" || n.State != "playing" {
		t.Fatalf("%+v", n)
	}
	n, ok = parseMacNowPlaying("music\tpaused\tÇıkmaz Sokak\tŞebnem\tAlbüm\t10.5\t200.25\t")
	if !ok || n.Dur != 200.25 || n.Art != "" || n.State != "paused" {
		t.Fatalf("music %+v", n)
	}
	for _, bad := range []string{"", "spotify\tstopped", "spotify\tplaying\t\tA\tB\t1\t2\t", "spotify\tplaying\tSwim", "spotify\tplaying\tspotify:ad:123abc\tA\tB\t1\t2\t",
		"music\tfast forwarding\tX\tA\tB\t1\t2\t", "spotify\tplaying\tSwim\tA\tB\t1\t2\tjavascript:alert(1)"} {
		if n, ok := parseMacNowPlaying(bad); ok && n.Art != "" && n.Art[:8] != "https://" {
			t.Fatalf("%q unsafe", bad)
		} else if ok && bad != "spotify\tplaying\tSwim\tA\tB\t1\t2\tjavascript:alert(1)" {
			t.Fatalf("%q accepted", bad)
		}
	}
	if n, _ := parseMacNowPlaying("spotify\tplaying\tSwim\tA\tB\t500\t100000\t"); n.Pos != 100 {
		t.Fatal("position past the end")
	}
}

func TestPickMacPlayer(t *testing.T) {
	sp := macNowPlaying{App: "spotify", State: "paused"}
	mu := macNowPlaying{App: "music", State: "playing"}
	if n, _ := pickMacPlayer(sp, mu, true, true); n.App != "music" {
		t.Fatal("the playing one wins")
	}
	sp.State = "playing"
	if n, _ := pickMacPlayer(sp, mu, true, true); n.App != "spotify" {
		t.Fatal("both playing: Spotify")
	}
	mu.State = "paused"
	sp.State = "paused"
	if n, _ := pickMacPlayer(sp, mu, true, true); n.App != "spotify" {
		t.Fatal("both paused: Spotify")
	}
	if _, ok := pickMacPlayer(sp, mu, false, false); ok {
		t.Fatal("none")
	}
}

func FuzzParseMacNowPlaying(f *testing.F) {
	f.Add("spotify\tplaying\tSwim\tChase Atlantic\tPhases\t42,25\t240000\thttps://i.scdn.co/x")
	f.Fuzz(func(t *testing.T, s string) {
		n, ok := parseMacNowPlaying(s)
		if ok && (n.Title == "" || n.Pos < 0 || n.Dur < 0 || (n.Dur > 0 && n.Pos > n.Dur) || (n.Art != "" && n.Art[:8] != "https://")) {
			t.Fatalf("%q → %+v", s, n)
		}
	})
}
