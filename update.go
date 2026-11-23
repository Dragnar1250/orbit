package main

// Automatic updates from the GitHub releases of Dragnar1250/orbit.
//
// Every release carries orbit-update.json: the version, where its Orbit.exe is, the exe's SHA-256
// and size — signed with Orbit's private key (Ed25519; only the maintainer has it). Orbit only
// installs a file whose fingerprint matches a manifest signed with that key, so even a fake file
// put on GitHub (or a changed download) is refused.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	updateRepo      = "Dragnar1250/orbit"
	updatePublicKey = "nQ2by0EEe0DKWjPEe+PWXZIjkL9cfCaF28TfAFhD4g0="
	updateMaxSize   = 60 << 20
)

var (
	updateManifestURL = "https://github.com/" + updateRepo + "/releases/latest/download/orbit-update.json"
	updateURLPrefix   = "https://github.com/" + updateRepo + "/releases/download/"
	updateKey         = mustKey(updatePublicKey)
)

func mustKey(s string) ed25519.PublicKey {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("bad update key")
	}
	return ed25519.PublicKey(b)
}

type updateManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Notes   string `json:"notes"`
	Sig     string `json:"sig"`
}

// signedText: exactly what the signature covers (the notes are not trusted for anything).
func (m *updateManifest) signedText() []byte {
	return []byte("orbit-update\n" + m.Version + "\n" + m.URL + "\n" + strings.ToLower(m.SHA256) + "\n" + strconv.FormatInt(m.Size, 10))
}

// versionLess: "1.17.0" < "1.18.0" (numbers, missing parts are 0; anything odd is not newer).
func versionLess(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 4; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func validVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 9999 || p != strconv.Itoa(n) {
			return false
		}
	}
	return true
}

// verify: signed by Orbit's key, a newer version, the exe on this repository's releases.
func (m *updateManifest) verify(current string, key ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(m.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(key, m.signedText(), sig) {
		return fmt.Errorf("the update isn't signed by Orbit — ignored")
	}
	if !validVersion(m.Version) {
		return fmt.Errorf("odd version %q", m.Version)
	}
	if !strings.HasPrefix(m.URL, updateURLPrefix) || strings.Contains(m.URL[len(updateURLPrefix):], "..") {
		return fmt.Errorf("the update isn't on Orbit's GitHub page")
	}
	if h, err := hex.DecodeString(m.SHA256); err != nil || len(h) != 32 {
		return fmt.Errorf("bad fingerprint")
	}
	if m.Size <= 0 || m.Size > updateMaxSize {
		return fmt.Errorf("bad size")
	}
	if !versionLess(current, m.Version) {
		return errUpToDate
	}
	return nil
}

var errUpToDate = fmt.Errorf("up to date")

var updateHTTP = &http.Client{Timeout: 5 * time.Minute}

// fetchManifest: the newest release's manifest, checked.
func fetchManifest() (*updateManifest, error) {
	req, _ := http.NewRequest("GET", updateManifestURL, nil)
	req.Header.Set("User-Agent", "Orbit/"+appVersion)
	cl := &http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errUpToDate // no release with an update file yet
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub said %d", resp.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&m); err != nil {
		return nil, fmt.Errorf("unreadable update file")
	}
	if err := m.verify(appVersion, updateKey); err != nil {
		return nil, err
	}
	return &m, nil
}

// downloadUpdate: the new exe into the update folder, checked against the signed fingerprint.
func downloadUpdate(m *updateManifest, progress func(float64)) (string, error) {
	dir := filepath.Join(appDir(), "update")
	_ = os.MkdirAll(dir, 0o755)
	final := filepath.Join(dir, "Orbit-"+m.Version+".exe")
	if st, err := os.Stat(final); err == nil && st.Size() == m.Size && fileSHA(final) == strings.ToLower(m.SHA256) {
		return final, nil // already here
	}
	req, _ := http.NewRequest("GET", m.URL, nil)
	req.Header.Set("User-Agent", "Orbit/"+appVersion)
	resp, err := updateHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download: GitHub said %d", resp.StatusCode)
	}
	part := final + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", fmt.Errorf("can't write the update")
	}
	h := sha256.New()
	var got int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			got += int64(n)
			if got > m.Size {
				f.Close()
				os.Remove(part)
				return "", fmt.Errorf("the download is bigger than it should be")
			}
			f.Write(buf[:n])
			h.Write(buf[:n])
			if progress != nil {
				progress(float64(got) / float64(m.Size))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return "", fmt.Errorf("download interrupted")
		}
	}
	f.Close()
	if got != m.Size || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(m.SHA256) {
		os.Remove(part)
		return "", fmt.Errorf("the download doesn't match Orbit's signature — not installed")
	}
	os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return "", fmt.Errorf("can't keep the update")
	}
	// only the newest one is kept
	old, _ := filepath.Glob(filepath.Join(dir, "Orbit-*.exe"))
	for _, p := range old {
		if p != final {
			os.Remove(p)
		}
	}
	return final, nil
}

func fileSHA(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// updater: what the settings show, and the background work.
type updaterState struct {
	sync.Mutex
	checking, downloading bool
	progress              float64
	avail                 *updateManifest
	ready                 string // the downloaded, checked exe
	err                   string
	lastCheck             time.Time
	upToDate              bool
}

var updater = &updaterState{}

func (u *updaterState) status() map[string]any {
	u.Lock()
	defer u.Unlock()
	st := map[string]any{"current": appVersion, "checking": u.checking, "downloading": u.downloading, "progress": u.progress,
		"error": u.err, "upToDate": u.upToDate, "ready": u.ready != "", "repo": updateRepo}
	if !u.lastCheck.IsZero() {
		st["lastCheck"] = u.lastCheck.Unix()
	}
	if u.avail != nil {
		st["version"], st["notes"] = u.avail.Version, u.avail.Notes
	}
	return st
}

// check (and download when asked or automatic). done(newReady) runs when an update is ready.
func (u *updaterState) check(download bool, done func()) {
	u.Lock()
	if u.checking || u.downloading {
		u.Unlock()
		return
	}
	u.checking, u.err = true, ""
	u.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				u.Lock()
				u.checking, u.downloading, u.err = false, false, fmt.Sprint("update: ", r)
				u.Unlock()
			}
		}()
		m, err := fetchManifest()
		u.Lock()
		u.checking, u.lastCheck = false, time.Now()
		switch {
		case err == errUpToDate:
			u.upToDate, u.avail = true, nil
		case err != nil:
			u.err = err.Error()
		default:
			u.upToDate, u.avail = false, m
		}
		go2 := m != nil && download && u.ready == ""
		if go2 {
			u.downloading, u.progress = true, 0
		}
		u.Unlock()
		if err != nil && err != errUpToDate {
			logf("update check: %v", err)
		}
		if !go2 {
			return
		}
		logf("update: downloading %s", m.Version)
		p, err := downloadUpdate(m, func(f float64) { u.Lock(); u.progress = f; u.Unlock() })
		u.Lock()
		u.downloading = false
		if err != nil {
			u.err = err.Error()
		} else {
			u.ready, u.progress = p, 1
		}
		u.Unlock()
		if err != nil {
			logf("update: %v", err)
			return
		}
		logf("update %s ready", m.Version)
		if done != nil {
			done()
		}
	}()
}

func (u *updaterState) readyFile() (string, string) {
	u.Lock()
	defer u.Unlock()
	if u.ready == "" || u.avail == nil {
		return "", ""
	}
	return u.ready, u.avail.Version
}

func (u *updaterState) fail(msg string) {
	u.Lock()
	u.err = msg
	u.Unlock()
}
