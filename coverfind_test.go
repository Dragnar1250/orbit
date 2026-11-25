package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testCover(n int, seed int) []byte {
	im := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			v := uint8((x*7/n + y*5/n*seed) * 23)
			im.Set(x, y, color.RGBA{v, uint8(255 - int(v)), uint8(seed * 60), 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, im, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

func TestFindBigCover(t *testing.T) {
	right, wrong := testCover(1200, 1), testCover(1200, 3)
	small, _, _ := image.Decode(bytes.NewReader(testCover(300, 1)))
	ref := sigFromImage(small)
	if d := sigDiff(ref, sigFromImage(mustDecode(right))); d > 0.05 {
		t.Fatal("same cover at two sizes differs", d)
	}
	if d := sigDiff(ref, sigFromImage(mustDecode(wrong))); d < 0.16 {
		t.Fatal("another cover looks the same", d)
	}
	var asked []string
	itunesOK := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		host := "http://" + r.Host
		switch {
		case r.URL.Path == "/search" && r.URL.Query().Get("entity") != "":
			if !itunesOK {
				fmt.Fprint(w, `{"results":[]}`)
				return
			}
			fmt.Fprintf(w, `{"results":[{"artistName":"Someone Else","artworkUrl100":"%s/it/wrong/100x100bb.jpg"},
				{"artistName":"The Neighbourhood","artworkUrl100":"%s/it/right/100x100bb.jpg"}]}`, host, host)
		case r.URL.Path == "/search":
			fmt.Fprintf(w, `{"data":[{"artist":{"name":"The Neighbourhood"},"album":{"cover_xl":"%s/dz/right/1000x1000-000000-80-0-0.jpg"}}]}`, host)
		case strings.Contains(r.URL.Path, "/1800x1800-"):
			w.WriteHeader(404)
		case strings.Contains(r.URL.Path, "/right/"):
			w.Write(right)
		case strings.Contains(r.URL.Path, "/wrong/"):
			w.Write(wrong)
		}
	}))
	defer srv.Close()
	itunesBase, deezerBase = srv.URL, srv.URL
	defer func() { itunesBase, deezerBase = "https://itunes.apple.com", "https://api.deezer.com" }()

	if b, _ := findBigCover("W.D.Y.W.F.M?", "The Neighbourhood", "I Love You.", ref); !bytes.Equal(b, right) {
		t.Fatal("iTunes: the artist's own cover", len(b), asked)
	}
	if b, _ := findBigCover("W.D.Y.W.F.M?", "The Neighbourhood", "", nil); !bytes.Equal(b, right) {
		t.Fatal("no player cover: the artist's own", len(b))
	}
	itunesOK = false
	asked = nil
	if b, _ := findBigCover("W.D.Y.W.F.M?", "The Neighbourhood", "I Love You.", ref); !bytes.Equal(b, right) {
		t.Fatal("Deezer when iTunes has none", len(b), asked)
	}
	otherRef := sigFromImage(mustDecode(wrong))
	if b, _ := findBigCover("W.D.Y.W.F.M?", "The Neighbourhood", "I Love You.", otherRef); b != nil {
		t.Fatal("a different picture must never be used")
	}
}

func mustDecode(b []byte) image.Image {
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	return im
}
