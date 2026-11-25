package main

// Spotify connection (optional): Windows only tells apps what is playing now, never what comes
// next. Spotify's own Web API does — so the user can connect their Spotify account once and full
// screen shows the real queue. Sign-in uses PKCE (no password or secret ever reaches Orbit; the
// refresh token stays in config.json on this PC).

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	spotifyAccounts = "https://accounts.spotify.com"
	spotifyAPI      = "https://api.spotify.com"
)

// read what plays and what's next; change the playback (play a song from search, add it to the
// queue, shuffle, repeat). Nothing else: no playlists, no library, no account details.
const spotifyScopes = "user-read-playback-state user-read-currently-playing user-modify-playback-state"

type queueItem struct {
	ID, Title, Artist, Image string
	File                     string // the picture, once downloaded
	Dur                      float64
	Album                    string
}

type spotifyState struct {
	sync.Mutex
	verifier, state string // the sign-in in progress
	access          string
	expires         time.Time
	queue           []queueItem
	queueFor        string // the playing song's title the queue belongs to
	queueAt         time.Time
	lastErr         string
	busy            bool
	scope           string // what Spotify allowed (from the last token)
	shuffle, repeat int    // Spotify's own answer (-1 unknown): used when Windows doesn't say
	modesAt         time.Time
	notes           []string // short messages for full screen ("Added to the queue", errors)
}

var spotify = &spotifyState{shuffle: -1, repeat: -1}

func randomURLSafe(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// spotifyRedirect: must match the Redirect URI saved in the user's Spotify app exactly.
func spotifyRedirect(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/spotify/callback", port)
}

// spotifyLoginURL starts a sign-in: Spotify's page, which comes back to /spotify/callback.
func (s *spotifyState) loginURL(clientID string, port int) string {
	s.Lock()
	defer s.Unlock()
	s.verifier = randomURLSafe(48)
	s.state = randomURLSafe(16)
	sum := sha256.Sum256([]byte(s.verifier))
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {spotifyRedirect(port)},
		"scope":                 {spotifyScopes},
		"code_challenge_method": {"S256"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"state":                 {s.state},
	}
	return spotifyAccounts + "/authorize?" + q.Encode()
}

type tokenResp struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Expires int    `json:"expires_in"`
	Scope   string `json:"scope"`
	Error   string `json:"error"`
	Desc    string `json:"error_description"`
}

func postToken(form url.Values) (tokenResp, error) {
	var t tokenResp
	cl := &http.Client{Timeout: 12 * time.Second}
	resp, err := cl.PostForm(spotifyAccounts+"/api/token", form)
	if err != nil {
		return t, err
	}
	defer resp.Body.Close()
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&t)
	if resp.StatusCode != 200 || t.Access == "" {
		if t.Desc != "" {
			return t, fmt.Errorf("%s", t.Desc)
		}
		return t, fmt.Errorf("Spotify said %d %s", resp.StatusCode, t.Error)
	}
	return t, nil
}

// finishLogin: Spotify sent the user back with a code — trade it for tokens.
func (s *spotifyState) finishLogin(clientID, code, state string, port int) (refresh, scope string, err error) {
	s.Lock()
	ver, want := s.verifier, s.state
	s.verifier, s.state = "", "" // one use
	s.Unlock()
	if want == "" || state != want {
		return "", "", fmt.Errorf("this sign-in link is old or not from Orbit — start again from the settings")
	}
	t, err := postToken(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {spotifyRedirect(port)},
		"client_id": {clientID}, "code_verifier": {ver}})
	if err != nil {
		return "", "", err
	}
	s.Lock()
	s.access, s.expires, s.scope = t.Access, time.Now().Add(time.Duration(t.Expires-60)*time.Second), t.Scope
	s.lastErr = ""
	s.Unlock()
	return t.Refresh, t.Scope, nil
}

// token: a valid access token, refreshed when needed. newRefresh is set when Spotify rotated it.
func (s *spotifyState) token(clientID, refresh string) (tok, newRefresh string, err error) {
	s.Lock()
	if s.access != "" && time.Now().Before(s.expires) {
		tok = s.access
		s.Unlock()
		return tok, "", nil
	}
	s.Unlock()
	if clientID == "" || refresh == "" {
		return "", "", fmt.Errorf("not connected")
	}
	t, err := postToken(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}})
	if err != nil {
		return "", "", err
	}
	s.Lock()
	s.access, s.expires = t.Access, time.Now().Add(time.Duration(t.Expires-60)*time.Second)
	if t.Scope != "" {
		s.scope = t.Scope
	}
	s.Unlock()
	return t.Access, t.Refresh, nil
}

// parseQueue reads Spotify's /me/player/queue answer.
func parseQueue(b []byte) (playing string, items []queueItem) {
	type img struct {
		URL   string `json:"url"`
		Width int    `json:"width"`
	}
	type track struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Dur     int    `json:"duration_ms"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			Name   string `json:"name"`
			Images []img  `json:"images"`
		} `json:"album"`
		Show struct { // podcast episodes
			Name   string `json:"name"`
			Images []img  `json:"images"`
		} `json:"show"`
		Images []img `json:"images"`
	}
	var r struct {
		Current *track  `json:"currently_playing"`
		Queue   []track `json:"queue"`
	}
	if json.Unmarshal(b, &r) != nil {
		return "", nil
	}
	if r.Current != nil {
		playing = r.Current.Name
	}
	pick := func(im []img) string { // the smallest picture of at least 160 px
		best, bw := "", 1<<30
		for _, i := range im {
			if i.Width >= 160 && i.Width < bw {
				best, bw = i.URL, i.Width
			}
		}
		if best == "" && len(im) > 0 {
			best = im[0].URL
		}
		return best
	}
	seen := 0
	for _, t := range r.Queue {
		it := queueItem{ID: t.ID, Title: t.Name, Dur: float64(t.Dur) / 1000, Album: t.Album.Name}
		var names []string
		for _, a := range t.Artists {
			names = append(names, a.Name)
		}
		it.Artist = strings.Join(names, ", ")
		it.Image = pick(t.Album.Images)
		if it.Artist == "" {
			it.Artist = t.Show.Name
		}
		if it.Image == "" {
			it.Image = pick(append(t.Images, t.Show.Images...))
		}
		if it.ID == "" || it.Title == "" {
			continue
		}
		items = append(items, it)
		if seen++; seen >= 10 {
			break
		}
	}
	return playing, items
}

// fetchQueue asks Spotify what plays next. Returns ok=false when it can't (not connected, no
// internet, a free account without that API, …) — the caller keeps what it had.
func (s *spotifyState) fetchQueue(clientID, refresh string) (items []queueItem, playing, newRefresh string, err error) {
	tok, nr, err := s.token(clientID, refresh)
	if err != nil {
		return nil, "", "", err
	}
	req, _ := http.NewRequest("GET", spotifyAPI+"/v1/me/player/queue", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "", nr, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode == 401 {
		s.Lock()
		s.access = "" // expired early: refresh next time
		s.Unlock()
		return nil, "", nr, fmt.Errorf("Spotify sign-in expired")
	}
	if resp.StatusCode == 403 { // Spotify's rule since Feb 2026: personal developer apps need Premium
		return nil, "", nr, fmt.Errorf("Spotify refused (personal Spotify apps need a Premium account)")
	}
	if resp.StatusCode == 204 { // nothing playing in Spotify right now
		return nil, "", nr, nil
	}
	if resp.StatusCode != 200 {
		return nil, "", nr, fmt.Errorf("Spotify queue: %d", resp.StatusCode)
	}
	playing, items = parseQueue(b)
	return items, playing, nr, nil
}

// queueCover: the picture of a queued song, downloaded once into the cache folder.
func queueCover(it queueItem) string {
	if it.Image == "" || !strings.HasPrefix(it.Image, "https://") {
		return ""
	}
	p := queueCoverPath(it)
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		return p
	}
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Get(it.Image)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 || len(b) == 0 {
		return ""
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if os.WriteFile(p, b, 0o644) != nil {
		return ""
	}
	return p
}

// snapshot: the last queue Spotify sent and the song it belongs to (nil when old or unknown).
func (s *spotifyState) snapshot() ([]queueItem, string) {
	s.Lock()
	defer s.Unlock()
	if s.queueAt.IsZero() || time.Since(s.queueAt) > 2*time.Minute {
		return nil, ""
	}
	return append([]queueItem(nil), s.queue...), s.queueFor
}

// stale: the queue no longer fits (Orbit skipped songs) — not shown until Spotify answers again.
func (s *spotifyState) stale() {
	s.Lock()
	s.queueAt = time.Time{}
	s.Unlock()
}

func queueCoverPath(it queueItem) string {
	sum := sha256.Sum256([]byte(it.Image))
	return filepath.Join(appDir(), "cache", "sq-"+hex.EncodeToString(sum[:8])+".jpg")
}

// refreshQueue runs in the background (one at a time).
func (s *spotifyState) refreshQueue() {
	s.Lock()
	if s.busy {
		s.Unlock()
		return
	}
	s.busy = true
	s.Unlock()
	var clientID, refresh string
	withCfg(func() { clientID, refresh = cfg.SpotifyClientID, cfg.SpotifyRefresh })
	go func() {
		defer func() {
			recover()
			s.Lock()
			s.busy = false
			s.Unlock()
		}()
		items, playing, nr, err := s.fetchQueue(clientID, refresh)
		if nr != "" {
			withCfg(func() { cfg.SpotifyRefresh = nr })
			saveConfigSoon()
		}
		if err != nil {
			s.Lock()
			if s.lastErr != err.Error() {
				logf("spotify: %v", err)
			}
			s.lastErr = err.Error()
			s.Unlock()
			return
		}
		for i := range items {
			if i < 8 {
				items[i].File = queueCover(items[i]) // fetched before it is shown: no empty squares
			}
		}
		s.Lock()
		s.queue, s.queueFor, s.queueAt, s.lastErr = items, playing, time.Now(), ""
		s.Unlock()
		prefetchLyrics(items) // the next songs' lyrics are ready before they start
		s.readPlayer()
	}()
}

// serveSpotify: sign-in start (with the settings token) and Spotify's return (checked by state).
func serveSpotify(w http.ResponseWriter, r *http.Request, port int) {
	switch r.URL.Path {
	case "/spotify/login":
		var id string
		withCfg(func() { id = cfg.SpotifyClientID })
		if id == "" {
			http.Error(w, "Paste your Client ID in Orbit's settings first.", 400)
			return
		}
		http.Redirect(w, r, spotify.loginURL(id, port), http.StatusFound)
	case "/spotify/callback":
		var id string
		withCfg(func() { id = cfg.SpotifyClientID })
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := func(ok bool, msg string) {
			col := "#3ddc84"
			if !ok {
				col = "#ff6b7a"
			}
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Orbit</title><body style="background:#0e0f1a;color:#fff;font:16px Segoe UI,sans-serif;display:grid;place-items:center;height:90vh;margin:0"><div style="text-align:center"><div style="font-size:42px;color:%s">%s</div><p>%s</p><p style="opacity:.6">%s</p></div>`,
				col, map[bool]string{true: "✓", false: "✕"}[ok], htmlEscape(msg), htmlEscape(T("You can close this tab.")))
		}
		if e := r.URL.Query().Get("error"); e != "" {
			page(false, T("Spotify sign-in was cancelled")+" ("+e+").")
			return
		}
		ref, scope, err := spotify.finishLogin(id, r.URL.Query().Get("code"), r.URL.Query().Get("state"), port)
		if err != nil {
			logf("spotify sign-in: %v", err)
			page(false, T("Couldn't connect:")+" "+T(err.Error()))
			return
		}
		withCfg(func() { cfg.SpotifyRefresh, cfg.SpotifyScopes = ref, scope })
		saveConfigSoon()
		logf("spotify connected")
		spotify.refreshQueue()
		page(true, T("Orbit is connected to your Spotify. Full screen now shows what plays next, and search plays songs right there."))
	default:
		http.NotFound(w, r)
	}
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// queueAfter: the part of a queue that comes after the playing song. Spotify read it while
// `playing` was on; if another song plays now and it is in that queue, what follows it still holds.
func queueAfter(q []queueItem, playing, nowTitle, nowKey string) []queueItem {
	if len(q) == 0 || normStr(playing) == normStr(nowTitle) {
		return q
	}
	for i, it := range q {
		if keyOf(it.Title, it.Artist) == nowKey {
			return q[i+1:]
		}
	}
	return nil
}

// canControl: connected, and allowed to change the playback (sign-ins from 1.17 only read).
func (s *spotifyState) canControl() bool {
	var id, ref, sc string
	withCfg(func() { id, ref, sc = cfg.SpotifyClientID, cfg.SpotifyRefresh, cfg.SpotifyScopes })
	if id == "" || ref == "" {
		return false
	}
	s.Lock()
	sc += " " + s.scope
	s.Unlock()
	return strings.Contains(sc, "user-modify-playback-state")
}

// connected: signed in at all (reading the queue works).
func spotifyConnected() bool {
	var ok bool
	withCfg(func() { ok = cfg.SpotifyClientID != "" && cfg.SpotifyRefresh != "" })
	return ok
}

func (s *spotifyState) note(msg string) {
	s.Lock()
	if len(s.notes) < 4 {
		s.notes = append(s.notes, msg)
	}
	s.Unlock()
}

// takeNotes: messages for full screen to show (and forget).
func (s *spotifyState) takeNotes() []string {
	s.Lock()
	defer s.Unlock()
	n := s.notes
	s.notes = nil
	return n
}

// call: one Web API request with the current token (refreshing it when needed).
func (s *spotifyState) call(method, path string, body io.Reader) (int, []byte, error) {
	var id, ref string
	withCfg(func() { id, ref = cfg.SpotifyClientID, cfg.SpotifyRefresh })
	tok, nr, err := s.token(id, ref)
	if nr != "" {
		withCfg(func() { cfg.SpotifyRefresh = nr })
		saveConfigSoon()
	}
	if err != nil {
		return 0, nil, err
	}
	req, _ := http.NewRequest(method, spotifyAPI+path, body)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode == 401 {
		s.Lock()
		s.access = ""
		s.Unlock()
	}
	return resp.StatusCode, b, nil
}

// spotifyErr: what a failed playback change means, in words.
func spotifyErr(code int, b []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Reason  string `json:"reason"`
		} `json:"error"`
	}
	json.Unmarshal(b, &e)
	switch {
	case code == 404 || e.Error.Reason == "NO_ACTIVE_DEVICE":
		return "Spotify isn't playing on any device — start it once"
	case code == 403 && strings.Contains(strings.ToLower(e.Error.Message), "premium"), e.Error.Reason == "PREMIUM_REQUIRED":
		return "Spotify only allows this with Premium"
	case code == 403:
		return "Spotify refused — connect again in Orbit's settings"
	case code == 401:
		return "Spotify sign-in expired — connect again in Orbit's settings"
	case code == 429:
		return "Spotify says: too many requests, try again in a moment"
	}
	return fmt.Sprintf("Spotify said %d", code)
}

// do: change the playback in the background. action: shuffle (arg "true"/"false"), repeat
// ("off"/"track"/"context"), play (arg = spotify:track:… — added to the queue and skipped to, so
// the album or playlist carries on after it), queue (arg = uri).
func (s *spotifyState) do(action, arg string) {
	if action == "shuffle" || action == "repeat" { // show the change at once
		s.Lock()
		if action == "shuffle" {
			s.shuffle = map[bool]int{true: 1, false: 0}[arg == "true"]
		} else {
			s.repeat = map[string]int{"off": 0, "track": 1, "context": 2}[arg]
		}
		s.modesAt = time.Now()
		s.Unlock()
	}
	go func() {
		defer func() { recover() }()
		ok := func(code int) bool { return code >= 200 && code < 300 }
		var code int
		var b []byte
		var err error
		switch action {
		case "shuffle":
			code, b, err = s.call("PUT", "/v1/me/player/shuffle?state="+url.QueryEscape(arg), nil)
		case "repeat":
			code, b, err = s.call("PUT", "/v1/me/player/repeat?state="+url.QueryEscape(arg), nil)
		case "queue", "play":
			code, b, err = s.call("POST", "/v1/me/player/queue?uri="+url.QueryEscape(arg), nil)
			if err == nil && ok(code) && action == "play" {
				time.Sleep(250 * time.Millisecond) // let the queue take it before skipping to it
				code, b, err = s.call("POST", "/v1/me/player/next", nil)
			}
			if err == nil && ok(code) {
				s.stale()
				time.AfterFunc(900*time.Millisecond, s.refreshQueue)
				if action == "queue" {
					s.note("Added to the queue")
				}
			}
		default:
			return
		}
		if err != nil {
			s.note("Couldn't reach Spotify")
			return
		}
		if !ok(code) {
			msg := spotifyErr(code, b)
			logf("spotify %s: %s (%s)", action, msg, strings.TrimSpace(string(b)))
			s.note(msg)
		}
	}()
}

// parsePlayer: shuffle and repeat from /v1/me/player.
func parsePlayer(b []byte) (shuffle, repeat int, ok bool) {
	var p struct {
		Shuffle *bool  `json:"shuffle_state"`
		Repeat  string `json:"repeat_state"`
	}
	if json.Unmarshal(b, &p) != nil || p.Shuffle == nil {
		return -1, -1, false
	}
	shuffle = map[bool]int{true: 1, false: 0}[*p.Shuffle]
	repeat = map[string]int{"off": 0, "track": 1, "context": 2}[p.Repeat]
	return shuffle, repeat, true
}

// modes: Spotify's shuffle / repeat (-1 when unknown).
func (s *spotifyState) modes() (int, int) {
	s.Lock()
	defer s.Unlock()
	if time.Since(s.modesAt) > 3*time.Minute {
		return -1, -1
	}
	return s.shuffle, s.repeat
}

func (s *spotifyState) readPlayer() {
	code, b, err := s.call("GET", "/v1/me/player", nil)
	if err != nil || code != 200 {
		return
	}
	if sh, rp, ok := parsePlayer(b); ok {
		s.Lock()
		if time.Since(s.modesAt) > 2500*time.Millisecond { // not right after the user changed one
			s.shuffle, s.repeat, s.modesAt = sh, rp, time.Now()
		}
		s.Unlock()
	}
}
