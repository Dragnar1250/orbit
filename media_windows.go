package main

// Now-playing info.
//  1) Windows media controls (WinRT GlobalSystemMediaTransportControlsSessionManager):
//     title, artist, cover, play state and the EXACT position — works with Spotify, browsers, …
//  2) Fallback: the Spotify window title ("Artist - Song"), cover from iTunes search,
//     position estimated from when the title changed.

import (
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	iidManagerStatics = GUID{0x2050C4EE, 0x11A0, 0x57DE, [8]byte{0xAE, 0xD7, 0xC9, 0x7C, 0x70, 0x33, 0x82, 0x45}}
	iidAsyncInfo      = GUID{0x00000036, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIStream        = GUID{0x0000000C, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// Windows.Storage.Streams.IRandomAccessStreamReference
	iidStreamRef = GUID{0x33EE3134, 0x1DD6, 0x4E3A, [8]byte{0x80, 0x67, 0xD1, 0xC1, 0x62, 0xE8, 0x64, 0x2B}}
)

var thumbsEnabled = true

const (
	asyncStarted   = 0
	asyncCompleted = 1
	statusPlaying  = 4
)

func hstr(s string) uintptr {
	w, _ := syscall.UTF16FromString(s)
	var h uintptr
	call(pWindowsCreateString, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)-1), uintptr(unsafe.Pointer(&h)))
	return h
}

func takeHString(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	p := call(pWindowsGetStringRawBuffer, h, uintptr(unsafe.Pointer(&n)))
	s := utf16PtrToString(p, int(n))
	call(pWindowsDeleteString, h)
	return s
}

// await waits for an IAsyncOperation<T> and returns its result (caller releases it).
func await(op uintptr, timeout time.Duration) (uintptr, bool) {
	if op == 0 {
		return 0, false
	}
	defer comRelease(op)
	var info uintptr
	if failed(comCall(op, 0, uintptr(unsafe.Pointer(&iidAsyncInfo)), uintptr(unsafe.Pointer(&info)))) || info == 0 {
		return 0, false
	}
	defer comRelease(info)
	deadline := time.Now().Add(timeout)
	for {
		var st int32
		if failed(comCall(info, 7, uintptr(unsafe.Pointer(&st)))) {
			return 0, false
		}
		if st == asyncCompleted {
			break
		}
		if st != asyncStarted || time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(3 * time.Millisecond)
	}
	var res uintptr
	if failed(comCall(op, 8, uintptr(unsafe.Pointer(&res)))) {
		return 0, false
	}
	return res, true
}

type SMTC struct {
	mgr      uintptr
	lastKey  string
	coverTry int
	fails    int
}

func newSMTC() *SMTC {
	call(pRoInitialize, 1) // multithreaded
	cls := hstr("Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager")
	defer call(pWindowsDeleteString, cls)
	var statics uintptr
	hr := call(pRoGetActivationFactory, cls, uintptr(unsafe.Pointer(&iidManagerStatics)), uintptr(unsafe.Pointer(&statics)))
	if failed(hr) || statics == 0 {
		logf("smtc: RoGetActivationFactory failed 0x%08X", uint32(hr))
		return nil
	}
	defer comRelease(statics)
	var op uintptr
	if failed(comCall(statics, 6, uintptr(unsafe.Pointer(&op)))) { // RequestAsync
		logf("smtc: RequestAsync failed")
		return nil
	}
	mgr, ok := await(op, 5*time.Second)
	if !ok || mgr == 0 {
		logf("smtc: manager not available")
		return nil
	}
	return &SMTC{mgr: mgr}
}

func (s *SMTC) appID(sess uintptr) string {
	var h uintptr
	comCall(sess, 6, uintptr(unsafe.Pointer(&h)))
	return takeHString(h)
}

// session prefers Spotify, otherwise whatever Windows considers "current".
func (s *SMTC) session() uintptr {
	var list uintptr
	if !failed(comCall(s.mgr, 7, uintptr(unsafe.Pointer(&list)))) && list != 0 { // GetSessions
		var n uint32
		comCall(list, 7, uintptr(unsafe.Pointer(&n))) // IVectorView.Size
		for i := uint32(0); i < n && i < 32; i++ {
			var sess uintptr
			if failed(comCall(list, 6, uintptr(i), uintptr(unsafe.Pointer(&sess)))) || sess == 0 { // GetAt
				continue
			}
			if strings.Contains(strings.ToLower(s.appID(sess)), "spotify") {
				comRelease(list)
				return sess
			}
			comRelease(sess)
		}
		comRelease(list)
	}
	var cur uintptr
	comCall(s.mgr, 6, uintptr(unsafe.Pointer(&cur))) // GetCurrentSession
	return cur
}

func getHString(obj uintptr, slot int) string {
	var h uintptr
	if failed(comCall(obj, slot, uintptr(unsafe.Pointer(&h)))) {
		return ""
	}
	return takeHString(h)
}

func getI64(obj uintptr, slot int) int64 {
	var v int64
	comCall(obj, slot, uintptr(unsafe.Pointer(&v)))
	return v
}

// poll reads the current state once. Returns false if nothing is playing via the media controls.
func (s *SMTC) poll() bool {
	sess := s.session()
	if sess == 0 {
		return false
	}
	defer comRelease(sess)

	var op uintptr
	if failed(comCall(sess, 7, uintptr(unsafe.Pointer(&op)))) { // TryGetMediaPropertiesAsync
		return false
	}
	props, ok := await(op, 3*time.Second)
	if !ok || props == 0 {
		return false
	}
	defer comRelease(props)
	title := getHString(props, 6)  // Title
	artist := getHString(props, 9) // Artist
	album := getHString(props, 10) // AlbumTitle
	if artist == "" {
		artist = getHString(props, 8) // AlbumArtist
	}
	if strings.TrimSpace(title) == "" {
		return false
	}

	isNew := media.SetTrack(title, artist, album, s.appID(sess)) // the app id lets the skin search in the right place
	key := keyOf(title, artist)
	if isNew || key != s.lastKey {
		s.lastKey, s.coverTry = key, 0
	}

	// timeline
	var tl uintptr
	playing := false
	var pb uintptr
	if !failed(comCall(sess, 9, uintptr(unsafe.Pointer(&pb)))) && pb != 0 { // GetPlaybackInfo
		var st int64                                 // enum is 32-bit; a wider slot keeps us safe
		comCall(pb, 7, uintptr(unsafe.Pointer(&st))) // PlaybackStatus
		st &= 0xFFFFFFFF
		playing = st == statusPlaying
		media.SetModes(readModes(pb))
		comRelease(pb)
	}
	if !failed(comCall(sess, 8, uintptr(unsafe.Pointer(&tl)))) && tl != 0 { // GetTimelineProperties
		start := getI64(tl, 6)
		end := getI64(tl, 7)
		pos := getI64(tl, 10)
		upd := getI64(tl, 11)
		comRelease(tl)
		dur := float64(end-start) / 1e7
		p := float64(pos) / 1e7
		if playing && upd > 0 {
			nowFT := time.Now().UnixNano()/100 + 116444736000000000
			if d := float64(nowFT-upd) / 1e7; d > 0 && d < 36000 {
				p += d
			}
		}
		if dur > 0 && dur < 7200 {
			media.SetDuration(key, dur)
			if p > dur {
				p = dur
			}
		}
		if dur > 0 || p > 0 {
			media.SetPosition(p, playing)
		} else {
			media.SetPlaying(playing)
		}
	} else {
		media.SetPlaying(playing)
	}

	// cover (only until we have one for this song)
	if s.coverTry < 4 && thumbsEnabled {
		snap := media.Snapshot()
		if snap.Track != nil && snap.Track.Key == key && snap.Track.Cover == nil {
			s.coverTry++
			if img := s.readThumbnail(props); len(img) > 0 {
				media.SetCover(key, img)
			} else if s.coverTry == 4 {
				t, a := title, artist
				go func() { media.SetCover(keyOf(t, a), fetchCoverOnline(t, a)) }()
			}
		}
	}
	return true
}

// thumbnailRef finds the Thumbnail property. Both candidate slots hold COM objects
// (Thumbnail / Genres), so asking each for IRandomAccessStreamReference is always safe.
func thumbnailRef(props uintptr) uintptr {
	for _, slot := range []int{15, 12} {
		var obj uintptr
		if failed(comCall(props, slot, uintptr(unsafe.Pointer(&obj)))) || obj == 0 {
			continue
		}
		var ref uintptr
		hr := comCall(obj, 0, uintptr(unsafe.Pointer(&iidStreamRef)), uintptr(unsafe.Pointer(&ref)))
		comRelease(obj)
		if !failed(hr) && ref != 0 {
			return ref
		}
	}
	return 0
}

func (s *SMTC) readThumbnail(props uintptr) []byte {
	ref := thumbnailRef(props)
	if ref == 0 {
		return nil
	}
	defer comRelease(ref)
	var op uintptr
	if failed(comCall(ref, 6, uintptr(unsafe.Pointer(&op)))) { // OpenReadAsync
		return nil
	}
	ras, ok := await(op, 3*time.Second)
	if !ok || ras == 0 {
		return nil
	}
	defer comRelease(ras)
	var stream uintptr
	if failed(call(pCreateStreamOverRandomAccessStream, ras, uintptr(unsafe.Pointer(&iidIStream)), uintptr(unsafe.Pointer(&stream)))) || stream == 0 {
		return nil
	}
	defer comRelease(stream)
	var out []byte
	buf := make([]byte, 65536)
	for len(out) < 8<<20 {
		var n uint32
		hr := comCall(stream, 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n))) // ISequentialStream.Read
		if failed(hr) {
			break
		}
		out = append(out, buf[:n]...)
		if n == 0 || hr == 1 {
			break
		}
	}
	return out
}

/* ---------- Fallback: Spotify window title ---------- */

var (
	enumFound    string
	enumSpotify  bool
	enumCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if call(pIsWindowVisible, hwnd) == 0 {
			return 1
		}
		t := windowText(hwnd)
		if t == "" {
			return 1
		}
		if !strings.HasSuffix(strings.ToLower(windowProcessName(hwnd)), `\spotify.exe`) {
			return 1
		}
		enumSpotify = true
		if strings.Contains(t, " - ") {
			enumFound = t
			return 0
		}
		return 1
	})
)

// titlePoll returns false when Spotify isn't running.
func titlePoll() bool {
	enumFound, enumSpotify = "", false
	call(pEnumWindows, enumCallback, 0)
	if !enumSpotify {
		return false
	}
	if enumFound == "" { // Spotify open but paused ("Spotify Premium")
		media.SetPlaying(false)
		return true
	}
	parts := strings.SplitN(enumFound, " - ", 2)
	artist, title := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if media.SetTrack(title, artist, "", "spotify-title") {
		media.SetPlaying(true)
		k := keyOf(title, artist)
		go func() { media.SetCover(k, fetchCoverOnline(title, artist)) }()
	} else {
		media.SetPlaying(true)
	}
	return true
}

/* ---------- Poll loop ---------- */

var (
	seekCh    = make(chan float64, 1)
	smtcReady atomic.Bool
)

// requestSeek jumps the playing song to sec (false when this PC's player can't be told).
func requestSeek(sec float64) bool {
	if !smtcReady.Load() {
		return false
	}
	media.Seek(sec)
	select { // only the newest jump matters
	case <-seekCh:
	default:
	}
	select {
	case seekCh <- sec:
	default:
	}
	return true
}

// seek: TryChangePlaybackPositionAsync (in 100 ns steps). The answer isn't waited for — the
// next poll shows where the player really is.
func (s *SMTC) seek(sec float64) {
	sess := s.session()
	if sess == 0 {
		return
	}
	defer comRelease(sess)
	var op uintptr
	if !failed(comCall(sess, 24, uintptr(int64(sec*1e7)), uintptr(unsafe.Pointer(&op)))) && op != 0 {
		comRelease(op)
	}
}

// readModes: shuffle and repeat from the playback info (IReference values, may be missing).
func readModes(pb uintptr) Modes {
	md := Modes{Shuffle: -1, Repeat: -1}
	var ctl uintptr
	if !failed(comCall(pb, 6, uintptr(unsafe.Pointer(&ctl)))) && ctl != 0 { // Controls
		var b uint32
		comCall(ctl, 17, uintptr(unsafe.Pointer(&b))) // IsShuffleEnabled
		md.CanShuffle = b&0xFF != 0
		b = 0
		comCall(ctl, 18, uintptr(unsafe.Pointer(&b))) // IsRepeatEnabled
		md.CanRepeat = b&0xFF != 0
		comRelease(ctl)
	}
	refVal := func(slot int) (int, bool) {
		var ref uintptr
		if failed(comCall(pb, slot, uintptr(unsafe.Pointer(&ref)))) || ref == 0 {
			return 0, false
		}
		defer comRelease(ref)
		var v uint32
		if failed(comCall(ref, 6, uintptr(unsafe.Pointer(&v)))) { // IReference<T>.Value
			return 0, false
		}
		return int(v), true
	}
	if v, ok := refVal(11); ok { // IsShuffleActive
		md.Shuffle = map[bool]int{true: 1, false: 0}[v&0xFF != 0]
	}
	if v, ok := refVal(9); ok && v >= 0 && v <= 2 { // AutoRepeatMode: None, Track, List
		md.Repeat = v
	}
	return md
}

type modeReq struct {
	shuffle bool // else repeat
	val     int
}

var modeCh = make(chan modeReq, 2)

// setMode: TryChangeShuffleActiveAsync (23) / TryChangeAutoRepeatModeAsync (21).
func (s *SMTC) setMode(r modeReq) {
	sess := s.session()
	if sess == 0 {
		return
	}
	defer comRelease(sess)
	var op uintptr
	slot := 21
	if r.shuffle {
		slot = 23
	}
	if !failed(comCall(sess, slot, uintptr(r.val), uintptr(unsafe.Pointer(&op)))) && op != 0 {
		comRelease(op)
	}
}

// requestShuffle / requestRepeat: through Windows when the player takes it, else through
// Spotify's own connection. false: no way to change it from here.
func requestShuffle(on bool) bool {
	v := map[bool]int{true: 1, false: 0}[on]
	if smtcReady.Load() && media.Snapshot().Modes.CanShuffle {
		media.WantModes(v, -1)
		select {
		case modeCh <- modeReq{shuffle: true, val: v}:
		default:
		}
		return true
	}
	if spotify.canControl() {
		media.WantModes(v, -1)
		spotify.do("shuffle", map[bool]string{true: "true", false: "false"}[on])
		return true
	}
	return false
}

func requestRepeat(mode int) bool {
	if smtcReady.Load() && media.Snapshot().Modes.CanRepeat {
		media.WantModes(-1, mode)
		select {
		case modeCh <- modeReq{val: mode}:
		default:
		}
		return true
	}
	if spotify.canControl() {
		media.WantModes(-1, mode)
		spotify.do("repeat", []string{"off", "track", "context"}[mode])
		return true
	}
	return false
}

func mediaLoop(useSMTC, thumbs bool) {
	thumbsEnabled = thumbs
	runtime.LockOSThread()
	defer func() {
		if r := recover(); r != nil {
			logf("media loop panic: %v — switching to Spotify title mode", r)
			go mediaLoop(false, false)
		}
	}()
	var s *SMTC
	if useSMTC {
		s = newSMTC()
		if s != nil {
			logf("media: using Windows media controls")
		}
	}
	if s == nil {
		logf("media: using Spotify window title")
	}
	smtcReady.Store(s != nil)
	for {
		if s != nil {
			if !s.poll() && !titlePoll() { // nothing in the media controls and no Spotify window
				media.SetTrack("", "", "", "")
			}
			select { // a jump on the time line is sent from here: this thread owns the media controls
			case sec := <-seekCh:
				s.seek(sec)
				time.Sleep(120 * time.Millisecond) // let the player move before reading it again
			case mc := <-modeCh:
				s.setMode(mc)
				time.Sleep(120 * time.Millisecond)
			case <-time.After(500 * time.Millisecond):
			}
		} else {
			if !titlePoll() {
				media.SetTrack("", "", "", "")
			}
			time.Sleep(1000 * time.Millisecond)
		}
	}
}
