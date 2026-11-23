package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// The page Edge shows behind full screen: the song's video, muted, without YouTube's buttons,
// kept in time with the song by asking Orbit four times a second.
const videoPageHTML = `<!doctype html><html><head><meta charset="utf-8">
<meta name="referrer" content="strict-origin-when-cross-origin">
<title>Orbit Video</title>
<style>
html,body{margin:0;height:100%;background:#000;overflow:hidden;cursor:none}
#wrap{position:fixed;inset:0;overflow:hidden}
/* a little larger than the screen: YouTube's title strip and corners fall outside */
#p{position:absolute;left:50%;top:50%;width:max(100vw,177.78vh);height:max(56.25vw,100vh);transform:translate(-50%,-50%) scale(1.14);pointer-events:none;opacity:0;transition:opacity .8s}
#p.on{opacity:1}
</style></head><body><div id="wrap"><div id="p"></div></div>
<script>
const tok = new URLSearchParams(location.search).get("t");
let player = null, cur = "", ready = false, lastReport = 0, errId = "";
const api = (method, body) => fetch("/api/video?t=" + tok, method ? { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) } : {}).then(r => r.json());
function onYouTubeIframeAPIReady() { tick(); }
function make(id, start) {
  ready = false;
  player = new YT.Player("p", { videoId: id, playerVars: { autoplay: 1, mute: 1, controls: 0, disablekb: 1, fs: 0, iv_load_policy: 3, modestbranding: 1, playsinline: 1, rel: 0, start: Math.max(0, Math.floor(start)), origin: location.origin },
    events: { onReady: () => { ready = true; player.mute(); }, onError: e => { errId = cur; api("POST", { error: e.data, id: cur }); } } });
}
async function tick() {
  try {
    const s = await api();
    const el = document.getElementById("p");
    if (s.id !== cur) {
      cur = s.id;
      if (!s.id) { if (player && ready) player.stopVideo(); el.classList.remove("on"); }
      else if (!player) make(s.id, s.pos + s.offset);
      else if (ready) player.loadVideoById({ videoId: s.id, startSeconds: Math.max(0, s.pos + s.offset) });
    }
    if (player && ready && cur) {
      player.mute();
      const target = s.pos + s.offset + 0.12, st = player.getPlayerState(), t = player.getCurrentTime(), dur = player.getDuration();
      const playing = st === 1;
      if (s.playing && target < dur - 0.5) {
        if (!playing && st !== 3) player.playVideo();
        if (Math.abs(t - target) > 0.7) player.seekTo(Math.max(0, target), true);
      } else if (playing) player.pauseVideo();
      el.classList.toggle("on", playing || st === 2 || st === 3);
      if (Date.now() - lastReport > 900) { lastReport = Date.now(); api("POST", { playing: playing || (st === 2 && !s.playing), id: cur, dur }); }
    }
  } catch (e) {}
  setTimeout(tick, 250);
}
</script>
<script src="https://www.youtube.com/iframe_api"></script>
</body></html>`

// serveVideo: the page and its little API (behind the same token as the settings).
func serveVideo(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/video":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(videoPageHTML))
	case r.Method == "POST":
		var rep struct {
			Playing bool    `json:"playing"`
			Error   int     `json:"error"`
			ID      string  `json:"id"`
			Dur     float64 `json:"dur"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&rep) != nil {
			http.Error(w, "bad", 400)
			return
		}
		video.Lock()
		if rep.ID == video.current() && rep.ID != "" {
			if rep.Error != 0 { // can't be embedded / removed: the next one
				logf("video %s can't play here (YouTube error %d), trying the next", rep.ID, rep.Error)
				video.skip()
			} else if rep.Playing {
				video.Seen = time.Now()
			}
		}
		video.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	default:
		snap := media.Snapshot()
		video.Lock()
		out := map[string]any{"id": "", "offset": video.Offset, "pos": snap.Pos, "playing": snap.Playing}
		if video.Want && snap.Track != nil && snap.Track.Key == video.Key {
			out["id"] = video.current()
		}
		video.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}
