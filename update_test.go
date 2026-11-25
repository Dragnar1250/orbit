package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		less bool
	}{{"1.17.0", "1.18.0", true}, {"1.18.0", "1.18.0", false}, {"1.18.0", "1.17.9", false}, {"1.9.0", "1.10.0", true},
		{"1.18", "1.18.1", true}, {"v1.18.0", "1.18.1", true}, {"2.0.0", "1.99.99", false}, {"1.18.0", "x", false}} {
		if versionLess(c.a, c.b) != c.less {
			t.Fatalf("%s < %s", c.a, c.b)
		}
	}
	for v, ok := range map[string]bool{"1.18.0": true, "1.18": true, "1": false, "1.x": false, "1.18.0-beta": false, "01.2": false, "1..2": false, "": false} {
		if validVersion(v) != ok {
			t.Fatalf("valid %q", v)
		}
	}
}

func signWith(priv ed25519.PrivateKey, m *updateManifest) {
	m.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m.signedText()))
}

func TestManifestVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	good := func() *updateManifest {
		m := &updateManifest{Version: "9.0.0", URL: updateURLPrefix + "v9.0.0/Orbit.exe", SHA256: strings.Repeat("ab", 32), Size: 8 << 20}
		signWith(priv, m)
		return m
	}
	if err := good().verify("1.18.0", pub); err != nil {
		t.Fatal(err)
	}
	// anything changed after signing is refused
	for name, change := range map[string]func(*updateManifest){
		"version": func(m *updateManifest) { m.Version = "9.0.1" },
		"url":     func(m *updateManifest) { m.URL = updateURLPrefix + "v9.0.0/Evil.exe" },
		"sha":     func(m *updateManifest) { m.SHA256 = strings.Repeat("cd", 32) },
		"size":    func(m *updateManifest) { m.Size++ },
		"sig":     func(m *updateManifest) { m.Sig = "AAAA" },
	} {
		m := good()
		change(m)
		if m.verify("1.18.0", pub) == nil {
			t.Fatalf("changed %s accepted", name)
		}
	}
	// another key
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	m := good()
	signWith(other, m)
	if m.verify("1.18.0", pub) == nil {
		t.Fatal("another key accepted")
	}
	// signed but wrong in itself
	for name, change := range map[string]func(*updateManifest){
		"elsewhere":   func(m *updateManifest) { m.URL = "https://evil.example/Orbit.exe" },
		"climb":       func(m *updateManifest) { m.URL = updateURLPrefix + "../../x/Orbit.exe" },
		"bad sha":     func(m *updateManifest) { m.SHA256 = "zz" },
		"huge":        func(m *updateManifest) { m.Size = 1 << 40 },
		"empty":       func(m *updateManifest) { m.Size = 0 },
		"odd version": func(m *updateManifest) { m.Version = "9.0.0; rm" },
	} {
		m := good()
		change(m)
		signWith(priv, m)
		if m.verify("1.18.0", pub) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	// not newer
	m = good()
	m.Version = "1.18.0"
	signWith(priv, m)
	if m.verify("1.18.0", pub) != errUpToDate {
		t.Fatal("same version")
	}
	m.Version = "1.2.0"
	signWith(priv, m)
	if m.verify("1.18.0", pub) != errUpToDate {
		t.Fatal("downgrade")
	}
}

// the signing tool, the real key file and the key built into Orbit belong together
func TestSigningToolMatchesBuiltInKey(t *testing.T) {
	keyFile := "/home/claude/keys/orbit-signing-key.txt"
	if _, err := os.Stat(keyFile); err != nil {
		t.Skip("no key file here")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "Orbit.exe")
	os.WriteFile(exe, []byte("pretend exe"), 0o644)
	out := filepath.Join(dir, "orbit-update.json")
	cmd := exec.Command("go", "run", "./tools/orbit-sign", "-key", keyFile, "-exe", exe, "-version", "99.0.0", "-notes", "hi", "-out", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	var m updateManifest
	b, _ := os.ReadFile(out)
	if json.Unmarshal(b, &m) != nil {
		t.Fatal("tool output")
	}
	if err := m.verify(appVersion, updateKey); err != nil {
		t.Fatalf("Orbit refuses what the tool signs: %v", err)
	}
	if m.URL != "https://github.com/Dragnar1250/orbit/releases/download/v99.0.0/Orbit.exe" || m.Notes != "hi" || m.Size != 11 {
		t.Fatalf("%+v", m)
	}
}

func TestUpdateDownloadAndCheck(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	exeBody := []byte(strings.Repeat("NEW ORBIT ", 50000))
	sum := sha256.Sum256(exeBody)
	tamper := false
	var fake *httptest.Server
	fake = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest/orbit-update.json":
			m := &updateManifest{Version: "9.1.0", URL: fake.URL + "/dl/v9.1.0/Orbit.exe", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(exeBody)), Notes: "new"}
			signWith(priv, m)
			json.NewEncoder(w).Encode(m)
		case "/dl/v9.1.0/Orbit.exe":
			b := exeBody
			if tamper {
				b = append([]byte(nil), exeBody...)
				b[100] = 'X'
			}
			w.Write(b)
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	oldURL, oldPrefix, oldKey := updateManifestURL, updateURLPrefix, updateKey
	updateManifestURL, updateURLPrefix, updateKey = fake.URL+"/latest/orbit-update.json", fake.URL+"/dl/", pub
	defer func() { updateManifestURL, updateURLPrefix, updateKey = oldURL, oldPrefix, oldKey }()

	m, err := fetchManifest()
	if err != nil || m.Version != "9.1.0" {
		t.Fatalf("%v %+v", err, m)
	}
	// a changed download is refused and nothing is kept
	tamper = true
	if _, err := downloadUpdate(m, nil); err == nil {
		t.Fatal("tampered exe accepted")
	}
	if files, _ := filepath.Glob(filepath.Join(appDir(), "update", "*")); len(files) != 0 {
		t.Fatalf("left behind %v", files)
	}
	tamper = false
	var last float64
	p, err := downloadUpdate(m, func(f float64) { last = f })
	if err != nil || last != 1 || fileSHA(p) != hex.EncodeToString(sum[:]) {
		t.Fatalf("%v %v", err, last)
	}
	// already downloaded: not again
	tamper = true
	if p2, err := downloadUpdate(m, nil); err != nil || p2 != p {
		t.Fatal("re-downloaded")
	}
	tamper = false
	// the whole background flow
	u := &updaterState{}
	done := make(chan bool, 1)
	u.check(true, func() { done <- true })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no update ready")
	}
	st := u.status()
	if st["version"] != "9.1.0" || st["ready"] != true || st["error"] != "" {
		t.Fatalf("%v", st)
	}
	if f, v := u.readyFile(); f == "" || v != "9.1.0" {
		t.Fatal("ready file")
	}
	// no release yet → "up to date", not an error
	updateManifestURL = fake.URL + "/nothing"
	u2 := &updaterState{}
	u2.check(false, nil)
	for i := 0; i < 100 && u2.status()["checking"] == true; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if st := u2.status(); st["upToDate"] != true || st["error"] != "" {
		t.Fatalf("%v", st)
	}
	// unreachable
	updateManifestURL = "http://127.0.0.1:1/x"
	u3 := &updaterState{}
	u3.check(false, nil)
	for i := 0; i < 200 && u3.status()["checking"] == true; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if st := u3.status(); st["error"] == "" {
		t.Fatalf("%v", st)
	}
	_ = fmt.Sprint()
}
