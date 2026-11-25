ORBIT 1.21  -  desktop music widgets for Windows 10 / 11
=============================================================

Sits on your desktop (behind your windows, on top of any wallpaper - Wallpaper
Engine, Lively or a normal picture) and shows:

  - a spinning turntable in a corner: picture disc (the cover IS the record) or a
    vinyl with the cover as label, tonearm that moves in as the song plays,
    now playing + recently played on an arc around it
  - synced lyrics that pop up in a RANDOM spot for every line (or a fixed spot)
    in 31 styles: Flow, Word reel, Cinematic, Kinetic, Typewriter, Neon, Subtitle,
    Stack, Rise, Pop, Karaoke, Scatter, Glass, Bounce, Wave, Glitch, Decode,
    Highlighter, Echo, Orbit, Slide, Spotlight, Rain, Swing, Split, Focus, Ink, Constellation, Bouncing,
    Split-flap, LED board
  - 12 text effects (glows, shadow, outline, neon, gradient, chromatic RGB split,
    long 3D shadow, hollow, soft blur, frosted glass) and 14 colour themes
  - a halo visualizer: the album cover with bars that react to the music

Works with Spotify and anything in the Windows media controls. Lyrics: lrclib.net


NEW IN 1.23.1  -  THE COVER BACKGROUND, SHARP AGAIN
---------------------------------------------------
  - Cinema's "Cover" background is drawn as in 1.22.4 again: straight at
    the screen's full size, with high-quality bicubic from the sharp
    2000 px cover - no smaller picture enlarged, no blur.

NEW IN 1.22.9  -  FASTER EVERYWHERE
-----------------------------------
  - Emptying a big picture before drawing it went through Windows (GDI+)
    and was by far the slowest step of the lyrics and the visualizer; it
    is a plain memory clear now. The full-screen lyrics draw about twice
    as fast.
  - Pulse, Stretch and Glow (the beat's motion) no longer redraw the words:
    the finished words are scaled and lit as they are laid on the screen,
    every frame, so they move smoothly with the beat at no cost.
  - Only the part of the stage that holds words is copied, glowed and
    pushed to the screen (most of it is empty).
  - The record's edge fade, the soft lights behind the record and the
    visualizer are quicker still.
  - Measured on the same test PC against 1.22.5, a frame in full screen
    takes 25-55 % less time, depending on the effect.

NEW IN 1.22.8  -  LIGHT ON THE PC
---------------------------------
  - The desktop widgets were heavy: the record's ambient light, the
    visualizer's glow and its hundreds of spokes, the groove light and the
    turning record were drawn with slow Windows (GDI+) calls 60 times a
    second, which on a big screen kept a core busy all the time and made
    the whole PC - the mouse too - stutter. They are drawn in Orbit's own
    fast code now (the same as full screen): the record is about 2.5x and
    the visualizer about 3x lighter.
  - Orbit runs below normal priority on the desktop (normal in full
    screen), so it never takes time from the mouse, games or other apps.
  - It uses at most half of the PC's cores for its pictures.
  - If a frame still takes too long (a slow PC, a 4K screen), the desktop
    steps its frame rate down by itself (60 > 45 > 30 > 20) and back up
    when there is room.
  - Fixed a rare endless loop when the groove light was drawn near the
    middle of the record.

NEW IN 1.22.7  -  SMOOTH AGAIN, SMALLER LEFT SIDE
------------------------------------------------
  - Full screen is smooth again: 1.22.6 redrew the big lyrics on almost
    every beat (Pulse / Stretch / Glow moved in very fine steps), which on
    big screens took the frame rate down. Back to the light steps, and the
    beat alone now redraws the words at most 30 times a second.
  - The song, the time line, the buttons and "Up next" are smaller again
    and the whole left side sits closer to the edge.
  - "UP NEXT" in full screen is translated too.

NEW IN 1.22.6  -  A SMOOTHER CINEMA
-----------------------------------
  - Smoother lyrics in full screen: a frame that takes a moment too long is
    picked up at the very next refresh instead of a whole frame later (the
    cause of the "15 fps" feeling, worst on 120/144 Hz screens). Wave and
    LED board move at the full frame rate there, and the music-driven
    Pulse / Stretch / Glow and the Dissolve haze move in fine steps.
  - The "Cover" background is high quality: drawn with high-quality
    bicubic from the sharp cover, frosted with a real soft blur - no steps
    or blocks, however big the screen.
  - The left side is smaller and calmer (title, artist, buttons) and sits
    further to the left, with more room before the lyrics.

NEW IN 1.22.5  -  TÜRKÇE, РУССКИЙ, AND A CALMER CINEMA
------------------------------------------------------
  - Languages: Settings > System > Language (at the top): English, Türkçe
    or Русский - or "Windows language", which follows Windows. Everything
    is translated: the settings, the tray menu, full screen (toasts and the
    H card), the widgets, the weather, the dates and the effect names
    (Constellation = Takımyıldız / Созвездие). The words clock speaks the
    language too ("saat onu beş geçiyor", "пять минут одиннадцатого").
  - Cinema's bar is simpler: a plain thin time line with a small knob, and
    plain white buttons - no frosted pill.
  - The "Cover" background is smooth now: the cover is blurred softly
    instead of being blown up from a tiny picture (no more blocky look).
  - Smoother full screen: the beat rings around the record are drawn much
    faster, still words are drawn once and reused, spark glows are reused.
  - "Big cover" is gone from Cinema (the cover or the record is enough);
    if you had it, you get the cover.

NEW IN 1.22.4  -  CINEMA: A NEW PLAYER, A BIGGER SKY
----------------------------------------------------
  - The controls on the left are redrawn: they sit on one frosted bar; the
    play button is a glowing disc in the album's colour; previous / next
    have rounded shapes; shuffle and repeat are drawn icons of the same
    weight (in the album's colour, with a dot, when on).
  - The time line is a little thicker, filled from the album's colour to
    white, with a knob that is always there and grows under the mouse.
  - Constellation in full screen: its words, stars and lines a size up.
  - (The test mode can now make up a beat, so what moves with the music is
    tested too.)

NEW IN 1.22.3  -  CLEANER WORDS, SHARPER PICTURES, LIGHTER FULL SCREEN
------------------------------------------------------------------------
  - Scatter: words never land on top of each other any more - each one is
    placed by its real size and tilt where it touches no other word, and a
    new line keeps clear of the one fading out. (Desktop widget and full
    screen.) Its layout is worked out once per line, not every frame.
  - Constellation in the desktop lyrics widget: a more compact sky with
    words about 1.6 times bigger and bigger stars; labels stay inside it.
  - Vinyl widget: the bars beside NOW PLAYING move with the music (they sway
    on their own when there is no sound to follow), and every few seconds a
    soft light runs across the words.
  - Covers: drawn with the best filter and no fringe at their edges; round
    corners and round record labels have smooth edges (they were cut with
    hard, stair-stepped edges); the big full-screen cover gets a touch of
    sharpening after it is resized.
  - Full screen uses less memory: the picture kept for cross-fades exists
    only around a song change, the old lyrics line is kept in reused memory
    instead of a new bitmap every line, and closing full screen gives back
    the outlines of its big words.

NEW IN 1.22.2  -  CINEMA: A TONEARM, STEADIER FRAMES
----------------------------------------------------
  - A tonearm on the full-screen record: it swings out to its rest when the
    music stops and comes back when it starts, lifting while it travels and
    settling gently into the groove, which it follows inwards as the song
    plays. It also lifts while records change. Silver, Black or Gold
    (A, or Settings > Cinema), or none.
  - Fixed: after a song change the cover / record could move to the middle
    and stay there (a song without lyrics). With lyrics on, the left side now
    always stays put; such a song shows "No lyrics for this song" or
    "Instrumental" on the right.
  - Frames now come in step with the screen's refresh (the display driver's
    own signal) and animations use that moment, so every step is the same
    size - no more uneven steps now and then. New choice in Settings >
    Cinema: "Screen's rate" draws a frame on every refresh of a 120/144 Hz
    screen.
  - Faster: each word's outline is made once instead of every frame, the
    next lyrics line is got ready while the current one is sung (no more
    hitch when a line starts), the LED board is about twice as light, and
    the glow skips the dark parts of the stage.
  - Q writes how even the frames are (median / 95% / worst) to orbit.log.

NEW IN 1.22.1  -  CINEMA: THE RECORD WORKS, EVERY EFFECT FILLS THE STAGE
-------------------------------------------------------------------------
  - Fixed: Vinyl in Cinema froze on its first frame (black screen, then
    "not responding"). It turns smoothly now, and the settings lock can no
    longer wait for itself anywhere in Cinema.
  - The record turns on every core of the processor instead of through
    GDI+: several times faster, sharper, and big. It uses the big room on
    the left, without the tonearm, and fades in at the edges when records
    change instead of being cut.
  - Every lyrics effect now fills the right half like Scatter and
    Constellation: each line is sized to the stage when it starts (short
    lines big, long ones over a few rows) and keeps that size while sung.
    Flow, Split-flap and the LED board are bigger too; Orbit's arc stays
    inside. The desktop lyrics widget is unchanged.
  - Flowing colours is about three times lighter; the layout no longer
    jumps to the middle for a moment when the song changes; the old line
    gets out of the way faster.
  - Q also writes what each part of a frame costs to orbit.log.
  - Stress-tested for about an hour: every effect, record, background, key,
    at 1366x768, 1080p, 1440p and 4K - no crash, no hang, no memory growth.

NEW IN 1.22  -  CINEMA: A BIGGER STAGE, REAL GLOW, A TURNING RECORD
--------------------------------------------------------------------
  - The lyrics stage takes the whole right half. Scatter and Constellation
    spread over all of it, every effect is about 45% bigger, and a line
    that ends fades and lifts away while the next one comes in.
  - Sharper words: Soft and Strong glow are now real light (a bloom drawn
    under the letters) instead of the old blurry halo, so the text itself
    stays crisp.
  - Backgrounds (B, or Settings > Cinema): Cover colours, Cover (the cover
    itself, darkened), Gradient, Dark, Flowing colours (they drift slowly).
  - Left side (V): Cover, Big cover (just the album art, as large as fits),
    or Vinyl - a turning record with the Vinyl widget's looks and effects
    (CD, worn, sparkles...) and the same record change when the song
    changes. R picks the record: like the Vinyl widget, or its own.
  - The lyrics on the desktop widget are unchanged - only Cinema's are new.
  - Q shows the time a frame takes, if anything feels slow.

NEW IN 1.21  -  CINEMA ONLY, A LYRICS STAGE, SMOOTH
---------------------------------------------------
  - Full screen is now Cinema alone: the Music display and the Desktop skin
    are gone (with the music-video background and the search bar they had).
  - The right half is a lyrics STAGE, from under the clock almost to the
    bottom. Pick the effect with Left / Right in full screen (or Settings >
    Cinema): Lines (Apple-style, line by line) or any of the 31 lyric styles -
    Scatter (the default) spreads every line's words over the whole stage,
    Kinetic, Karaoke, Constellation, Bouncing, Glitch, Split-flap, LED board...
    Font, size (Up / Down), text effect (E) and colours (T) are Cinema's own.
  - Previous / next song: the buttons, or Ctrl + Left / Right.
  - Smooth: it no longer stutters at ~15 fps. Lyrics were dimmed with a GDI+
    colour matrix - very slow; they are now blended by Orbit itself (8 lines
    at 1440p in about 2 ms, on all cores), and the stage redraws only where
    its words were and are. Cinema runs at 60 fps (Settings: 30 to save power).
  - Messages show above the cover; the left column is unchanged.

NEW IN 1.20  -  CINEMA: A NEW FULL SCREEN
-----------------------------------------
  Full screen (Ctrl+Alt+F) now opens in Cinema - calm, sharp and smooth:
  - The cover, large and sharp (the 2000 px one when the player's is small),
    with a deep soft shadow and a faint glow in its own colour.
  - Under it the song, the artist and the album; a thin time line (it
    thickens under the mouse - click or drag to jump); shuffle, previous,
    a white play / pause disc, next, repeat; and "Up next" from your
    Spotify queue (click it to skip there).
  - On the right the lyrics, line by line: the line being sung is bright
    and fills up word by word, the others are soft and fade toward the
    edges, and the lines glide up as the song goes on. Long pauses show
    three dots that fill until the singing starts again. No lyrics: the
    cover moves to the middle.
  - The background is the cover's own colours, blurred into soft light,
    darker where the text sits, with a fine grain so it never bands.
  - A small clock top right. No key bars on screen: H shows a card with
    all the keys. Song changes cross-fade.
  - Fast: Cinema is an ordinary window (the old views are see-through
    layered windows - Windows blended the whole 2560x1440 picture on
    every frame, that was the lag). Everything that stands still is drawn
    once per song; each frame only the lyrics, the time line and the
    controls are redrawn and copied to the screen. It doesn't listen to
    the sound at all, and nearly rests while paused. Q shows the time a
    frame takes.
  - Keys: Space play/pause, Left/Right previous/next, Up/Down lyrics size,
    L lyrics, C clock, H keys, D the other two views, S settings, mouse
    wheel volume, Esc / double-click leaves.
  - Settings > Full screen: Cinema / Music display / Desktop skin. Lyrics
    font, size and bold come from "Lyrics look" there.
  Vinyl
  - The playing song on the wheel has no box any more: its cover is bigger
    and lifted on a soft shadow with a glow in the album's colour, three
    little playing bars sit before NOW PLAYING, and the other songs step
    back (dimmer).

NEW IN 1.19.4  -  A QUIETER, ALBUM-TINTED HIGHLIGHT
---------------------------------------------------
  - The playing song on the wheel sits on quiet glass instead of an orange
    box: a faint album-coloured wash, a hairline edge, a thin highlight
    along the top and a soft shadow; its cover floats a little above it.
  - NOW PLAYING / UP NEXT / PLAYED and the playing song's artist take the
    album's colour - softened so they always read on the dark - and change
    with every cover.
  - Titles: Segoe UI Semibold for the playing song, regular for the rest.

NEW IN 1.19.3  -  A NEW SONG WHEEL, SHARP AT ANY SIZE
-----------------------------------------------------
  - The song wheel puts the song playing in the MIDDLE, highlighted: a bigger
    cover, bright bold title, a soft glowing pill behind it. Above it the
    next 2 songs (Spotify queue), below it the 2 that played - dimmer. The
    order is the same in every corner. (Vinyl > "Songs around it on the
    wheel": half above, half below; 4 = 2 and 2.) Not connected to Spotify:
    the top slot says so instead of guessing.
  - Sharp when small: the wheel's text is drawn grid-fitted (crisp edges),
    never smaller than a readable size, not tilted on a small wheel, and
    placed on whole pixels; rows spread out when the wheel is small so
    nothing overlaps. A small record turns with the sharper filter.
  - "Recently played" (Vinyl > View) keeps the old wheel.

NEW IN 1.19.2  -  UP NEXT ON THE RECORD, A FLOWING CHANGE, TIGHTER LYRICS
-------------------------------------------------------------------------
  Vinyl
  - The song wheel shows what plays NEXT ("Up next" under the playing
    song) - read from your real Spotify queue, refreshed when the song
    changes and every 20 s. Windows only tells apps what plays now, so this
    needs your Spotify account connected once (Full screen > Desktop skin >
    Spotify queue; Spotify allows it on Premium). Not connected: the wheel
    shows what you played, as before. Vinyl > View: Up next / Recently played.
  - Lift & drop flows: the new record starts coming down while the old one
    is still fading out (as soon as its cover is there) - no empty pause.
    The new record speeds up gently, the needle swings on as it settles.
  - Smoother: the record change no longer stutters at its start. The small
    cover copy for the song list is saved a few seconds later, the CD
    rainbow is made once per size instead of every song, the new record
    is drawn only once (when its cover is there), and during the change only
    the part of the window the records move over is redrawn.
  - Skipping several songs quickly mid-change just waits for the newest one.
  Lyrics
  - Steadier timing: the player's position used to be applied with a small
    jump twice a second, so words could twitch back and forth. Small
    differences are now eased in over 0.6 s (time only slows, never runs
    backwards); real jumps (seeking) are still followed at once.
  - Lyrics you already had show the moment the song starts (it used to wait
    1.2 s for the song length first).
  - With Spotify connected, the next two songs' lyrics are fetched ahead,
    so they are there from the first line.
  - Still a little early or late with your speakers / Bluetooth headphones?
    Lyrics > Lyrics offset moves them in 0.1 s steps.

NEW IN 1.19.1  -  A SMOOTHER RECORD CHANGE
------------------------------------------
  - New default "Lift & drop": when the song changes the needle lifts, the
    record slows down, lifts off (its shadow falls away) and fades out toward
    the corner; the next record comes down, fades in, settles with a soft
    little landing and speeds up again, then the needle drops back on.
  - The change now WAITS for the next song's cover (up to about 2 seconds)
    before the new record appears - no more wrong cover or the note sign
    flashing on the new record.
  - Fixed: the Sleeve change could show the cover of an earlier song (two
    songs with the same cover number shared one sleeve).
  - Choose the look in Vinyl > Behaviour: Lift & drop or Sleeve.

NEW IN 1.19  -  CD, SLEEVES, SCRATCHING, NEW VISUALIZERS AND BOARD LYRICS
-------------------------------------------------------------------------
  Vinyl
  - 4 new records: CD (holographic) - a silver disc whose rainbow drifts as
    it turns and flashes on the bass; Neon - black vinyl with glowing rings;
    Two-tone - half and half; Glow in the dark - phosphor green.
  - Change records with the song: when the song changes the needle lifts,
    the album sleeve slides over the record and away with it, the next
    song's sleeve slides in and off again, leaving the new record, and the
    needle drops back on. (Vinyl > Behaviour, on by default.)
  - Old, well-played record: dust and fine scratches on the record and tiny
    sparkles where the needle runs over them. (Vinyl > Effects, off by default.)
  - Scratch: hold the record on your desktop and turn it by hand - the song
    goes forward or back with it and the time shows on the record. One full
    turn = 6 seconds of the song (2-30, Vinyl > Behaviour). A click without
    turning still plays / pauses. To MOVE the record, drag it by the song
    wheel / card, or use arrange mode (Ctrl+Alt+E). Needs Windows media
    controls (Settings > Music) like the time line in full screen.
  Visualizer
  - Particles: particles in the album's own colours burst out on every kick
    and spring back to their ring (choose "Album colours" for the cover's
    colours, other themes use their palette).
  - Oscilloscope: the real waveform as a green phosphor trace on a little
    CRT, with afterglow, flicker and scanlines.
  - Liquid: a soft, glossy blob around the cover - the bass makes the big
    slow bulges, the highs the small quick ones, and it swells on the kick.
  Lyrics
  - Split-flap: a departures board. Every letter sits on its own tile and
    flips through other letters before it settles, left to right.
  - LED board: the line scrolls across a dot-matrix panel so the word being
    sung stays in view; sung words are lit, the current word brightest.

  Note: this exe is not signed with the update key, so Automatic updates
  won't offer it - just start the new Orbit.exe (it closes the old one).

NEW IN 1.18  -  SEARCH WITH SUGGESTIONS, SHUFFLE, AUTOMATIC UPDATES
--------------------------------------------------------------------
  - Search inside full screen: type in the bar's search box and songs show
    up as you type - the ones you played first, then Spotify's results
    (Apple Music's when Spotify isn't connected). Up/Down + Enter or a click
    plays the song right there; the + adds it to your queue. The album or
    playlist carries on after it. Without Spotify, Enter opens the song in
    your music app.
  - Shuffle and repeat buttons beside previous / next (repeat: off > all >
    this song). They light up when on.
  - Automatic updates from github.com/Dragnar1250/orbit (Settings > System >
    Updates). Every update is signed; Orbit refuses any file whose
    signature or fingerprint doesn't match. It installs when full screen is
    closed and restarts in about a second. If a new version ever fails to
    start, Orbit goes back to the old one by itself.
  - Spotify sign-in is now locked with Windows' own encryption (DPAPI): a
    copied config.json is useless on another PC or Windows account.
  - Spotify connection, once more: since 1.18 Orbit also asks permission to
    play/queue songs and switch shuffle/repeat (still nothing else - no
    playlists, library or account details). Connected in 1.17? Press
    "Connect again" once.
  - Note: Orbit must be in a folder you can write to (not Program Files)
    for updates to install themselves.

NEW IN 1.17  -  WHAT PLAYS NEXT, A SMOOTH FULL SCREEN, A REAL PLAYER BAR
-----------------------------------------------------------------------
  - Smooth again: moving the mouse no longer drops full screen to 2-3 frames
    a second while a song plays (frames were held back for every mouse move),
    and pointing at a cover no longer redraws the whole cover flow.
  - Up next: the list on the left and the covers on the right now show the
    songs that play NEXT. Windows only tells apps what plays now, so connect
    your Spotify account once (Settings > Full screen > Desktop skin >
    Spotify queue) and Orbit reads your real queue. Click a song in it to
    skip straight to it. Not connected: the list shows what you played.
  - Connecting Spotify (2 minutes, once):
      1. developer.spotify.com/dashboard > Create app (any name).
      2. Tick "Web API". Redirect URI: exactly the address shown in Orbit's
         settings (http://127.0.0.1:47330/spotify/callback), with Copy.
      3. Copy the app's Client ID into Orbit, press Connect Spotify, allow.
    Spotify allows these personal apps only on Premium accounts (their rule
    since February 2026). Without it the list shows what you played.
    No password reaches Orbit; the sign-in stays on this PC, locked
    (see 1.18).
    Disconnect any time in the same place.
  - The player bar does more: click or drag the time line to jump anywhere
    in the song (lyrics jump with it), and the mouse wheel over the bar
    changes the volume (Windows' own volume sign shows).

NEW IN 1.16  -  MUSIC VIDEOS, A 3D DESKTOP SKIN, SWITCH SONGS IN PLACE
---------------------------------------------------------------------
  - Music video in the background: a new background "Music video" (the
    Desktop skin's default, also in the Music display). Orbit finds the
    song's official video on YouTube and plays it muted behind full
    screen, in time with the song (it follows play / pause and seeking).
    How: Microsoft Edge (part of Windows) is opened as a hidden, borderless
    window with its own profile in Orbit's folder, tucked right under full
    screen, and closed again with it. Keys: N another video, , and . move
    the video half a second earlier / later (remembered per song). No video
    found, no internet, or YouTube refuses one: the blurred cover shows and
    the next candidate is tried. YouTube may show an ad first now and then.
  - "Continue playing" sits on a panel tilted back in real 3D perspective.
  - The middle shows seven covers: three songs before the playing one,
    the playing one, and three on the right - the songs that come next
    when Orbit knows them (after you went back), otherwise earlier ones.
    All in real perspective, drawn by Orbit itself (sharper than before).
  - Switch songs right there: click a song in Continue Playing or a card
    in the middle and Orbit steps your player back / forward to it with
    the player's own Previous / Next. Nothing else opens. The arrow keys
    and the bar's buttons keep the list in order the same way.
  - Covers on each side: 1, 2 or 3 (Full screen > Desktop skin > Middle).
  - A new player bar: the song on the left, a big round play button in
    the middle with the time line and times under it, search and the
    music app's dot on the right; frosted, with a soft shadow.
  - Crisper text: grid-fitted anti-aliasing in full screen.

NEW IN 1.15  -  REAL CONSTELLATIONS, BETTER TIMING, LIGHT MODE
--------------------------------------------------------------
  - Constellation lyrics use real constellations: Ursa Major, Orion,
    Cygnus, Leo, Scorpius, Lyra, Gemini, Ursa Minor, Pegasus, Draco,
    Sagittarius, Corona Borealis, Aquila, Taurus, Cassiopeia & Cepheus,
    Crux & Centaurus. One per lyric line, each with 6 to 11 lines - no more
    15-20 crossing lines. The words ride its stars (a long line puts a few
    words on a star), the sky waits faintly, lights up as it's sung, and
    shows its name when complete.
  - Timing: lines with only a start time get their words spread by
    syllables, with a breath after commas, using more of the time before
    the next line in slow songs (words came too early there) without
    stretching over instrumental breaks. Styles that fly / pop / rise
    words in start them a moment early so the word is readable on the
    beat. Typewriter types each word over its own time instead of an even
    pace (it ran ahead or behind before).
  - Dark / light mode for the settings window: System > Appearance (Like
    Windows, Dark, Light) or the sun / moon button at the top right. Like
    Windows follows the Windows setting as it changes, and the choice is
    remembered so the window opens in it without a flash.
  - Desktop skin: clicking a song in Continue Playing or the cover no
    longer opens Spotify - the cover and the song in the player bar play /
    pause right there. Only a search you type opens your music app.

NEW IN 1.14  -  ORBIT, WITH ITS OWN LOGO
----------------------------------------
  - The app is now simply "Orbit", with the Halo Bars logo: on Orbit.exe in
    File Explorer, in the taskbar and Alt+Tab (full screen), in the tray,
    on the settings window's tab and header. The exe also carries its name
    and version (right-click > Properties > Details). The tiny sizes use a
    simpler drawing so the logo stays clear at 16 px.
  - Your settings stay where they were. "Start with Windows" follows the
    new exe name by itself the first time Orbit.exe runs.
  - Full screen looks better: the background is stretched in two smooth
    steps with a fine dither (no more steps / bands in dark gradients),
    covers are fetched at 2000 px, and full screen only drops below full
    resolution on a really slow PC (never below 75 %).

NEW IN 1.13  -  A PRETTIER DESKTOP SKIN, SHARP FROM THE START
-------------------------------------------------------------
  - "Continue playing" floats on the left with no box: the playing cover,
    "Continue Playing", "Up next on <your app>", covers with soft shadows,
    and only the playing (or pointed at) song on a frosted pill.
  - Cover flow is the default middle: frosted cards with the cover and its
    name, the side cards turned toward the middle and darker further out,
    page dots underneath.
  - Full screen is drawn at your screen's full resolution again (sharp
    text and covers). Only if a PC is too slow does it draw smaller - and
    it goes back to full sharpness as soon as there is time.
  - Big covers no longer take 2-3 seconds to appear: they are fetched in
    the background while a song plays, kept on disk (the newest 300), and
    shown at once when full screen opens or the song comes back.

NEW IN 1.12.2  -  CLEANER POINTING
----------------------------------
  - Pointing at a widget no longer draws a frame or corner marks: only a
    small grip in the bottom-right corner - pull it to resize (scrolling
    still works too). Drag anywhere else on it to move it.
  - The buttons are smaller and sit in the top-right corner.
  - Double-clicking a widget switches its colours instead of opening the
    settings (that has its own button): lyrics and record - colour theme,
    visualizer - colour palette, clock - colour, weather - look. A small
    note shows what you got.
  - Songs whose "title" was really a file hash (79ead9f36e7f...) are no
    longer added to the song list, and old ones are removed.

NEW IN 1.12.1  -  FULL SCREEN, MUCH FASTER
------------------------------------------
  Full screen could crawl at 1-2 frames a second and hardly react to Esc.
  - The background used to be stretched to the whole screen by GDI+ every
    frame - the biggest cost by far. It is now stretched by Orbit itself on
    all CPU cores, at most 24 times a second, and simply copied otherwise.
  - The big soft glows behind the cover / card are drawn small together
    with the background instead of full size every frame; the cover is
    copied, not resampled; frosted panels use the small background.
  - Big screens (1440p, 4K) are drawn at about 1080p and stretched up.
  - If a PC still can't keep up, full screen notices within a few seconds
    and draws smaller (down to half size) until it can.
  - Keys and clicks come first: a slow frame can no longer hold up Esc.
  - Q shows the speed on screen; orbit.log gets "ms a frame" every 3 s.
  - Leaving full screen no longer pages the whole app out (that made the
    desktop stutter right after); the periodic memory trim now waits until
    the music is paused.

NEW IN 1.12  -  FULL SCREEN: DESKTOP SKIN
-----------------------------------------
  Full screen now has two kinds, chosen at the top of its page (or D in
  full screen to switch): "Music display" (1.11) and the new "Desktop skin":

  - The cover sharp across the whole screen (or blurred, Black hole, Aurora,
    Nebula, Colour flow, Starfield, Plain) with a Darken slider.
  - A tall, bright clock in the top-right with the date above it: Tall,
    Thin, Heavy or Serif; Album glow, White, Sunset, Ice or Neon gradient.
  - "Continue playing" on the left: the songs you played with their covers.
    (In 1.15: clicking no longer opens the music app.)
  - In the middle a frosted now playing card, a cover flow of your recent
    songs, or nothing.
  - A glass player bar at the bottom: previous, play/pause, next (they work
    with any player), the song and its progress, and a search box. Search
    with Spotify (app), Spotify Web Player, Apple Music, YouTube Music,
    SoundCloud, Amazon Music, YouTube - or Automatic, which follows the app
    that is playing. Click the coloured dot to switch.
  - Big lyrics drifting across the open part of the screen in their own
    style (Kinetic, Pop, Bouncing, Scatter, Constellation...).
  - The edge light around the screen in any of the 10 styles.
  - Keys: Space play/pause, Left/Right previous/next song, / or K search,
    B background, M middle, C clock, T clock colour, P Continue playing,
    L lyrics, F edge light, G its style, D music display, Esc leave.

  Faster and lighter
  - Full screen draws its background at most 24 times a second (a sharp
    cover only once per song) and copies it the rest of the time; panels,
    clock and player bar are drawn once and only redrawn when they change.
  - Full screen drops to 6 fps when nothing moves (paused, mouse still) and
    to 15 fps when another window is in front.
  - The desktop widgets drop to 10 fps when the music is paused, 5 fps with
    nothing playing, and 2 fps while a full-screen game covers your only
    screen - back to full speed the moment anything moves.
  - Audio capture stops when nothing on screen reacts to the music.
  - Turned-off widgets give their memory back; closing full screen hands
    its buffers back to Windows right away; a tighter Go memory limit.

  Crash tests
  - Thousands of random / broken / hostile settings requests from several
    threads at once, random config files, fuzzing of the lyrics parser and
    the search links, and random sizes for every layout. This found and
    fixed a crash in the Scatter style when a line's box had no height.
  - A cover used by a panel can no longer be freed while it is being drawn,
    and a widget whose bitmap can't be made skips a frame instead of crashing.

NEW IN 1.11  -  FULL SCREEN
--------------------------
  Turn a whole screen into a music display. Start it from the new "Full
  screen" page in the settings, the Go button on its Widgets card, the tray
  menu, or Ctrl+Alt+F from anywhere. Esc, F11 or a double-click leaves it.

  - Middle: Album art (sharp, rounded, glows and breathes with the bass),
    Vinyl (a big spinning record in your Vinyl look - tonearm, groove light,
    shine), Visualizer (any visualizer style and palette) or Lyrics only.
  - Backgrounds: Album art blur (slow Ken Burns drift), Black hole (a tilted
    disc of light circling a dark shadow, the far side bent over the top,
    faster on the kick), Aurora, Nebula, Colour flow, Starfield (warp speed
    on the bass) and Plain. Soft vignette on all of them.
  - High-resolution covers: a 1200 px cover is fetched when the player's is
    small - and only used if it really looks like the player's cover.
  - Bass ripple: a ring of light leaves the middle on every kick.
  - Its own lyrics: any of the 29 styles (Glass, Bouncing, Scatter,
    Constellation, Karaoke, Focus, Echo... picked out at the top), any
    installed font, size, bold, 12 text effects, colour themes or your own
    colours, line width, and where: under / over the middle, at the top, or
    wandering to a new spot every line. The desktop lyrics are not touched.
  - Around the screen: a frame of light running round the edges in any of
    the 10 Screen glow styles, with its own colour, brightness and reach.
  - Song info (bottom left with cover and time, or under the middle), a
    big quiet clock, 30 or 60 fps, the screen under the mouse or the main one.
  - Keys: A mode, B background, V visualizer, Left/Right lyrics style,
    Up/Down or the mouse wheel lyrics size, L lyrics, P lyrics place,
    E text effect, T colours, F frame, G frame style, C clock, I song info,
    R ripple, Space play/pause, S settings, H shows the keys, right-click
    the Orbit menu. The mouse pointer hides after a few seconds.
  - While full screen is open the desktop widgets rest (no extra CPU).

NEW IN 1.10
-----------
  - Point and move: just put the mouse on a widget on your desktop - no
    double-click. Drag it to move it, pull a corner or scroll to resize it.
    Small boxes appear on top of it (with their name when you point at one):
      Lyrics      - Settings, Hide it
      Vinyl       - Next look, Themes, Colors, Settings, Hide it
      Visualizer  - Next look, Themes, Color, Settings, Hide it
      Clock, Weather - Next look, Settings, Hide it
    Settings opens that widget's own page. Double-click a widget: its page.
    (System > Mouse turns pointing off; then double-click to arrange.)
  - Vinyl "Corner" layout: about half of the record tucked into a corner of
    the screen, the needle on it and a Now playing card with the cover beside
    it. Drag it and it snaps to the nearest corner. All records, colours and
    effects work as before.
  - Ambient light: a small soft light behind the record and the visualizer.
    Album colours, lyrics theme, your own colour or a slow rainbow; Still,
    Breathe, Beat, Flicker or Drift; brightness and size.

NEW IN 1.9
----------
  - Constellation: every word becomes a star. Thin lines join each new star
    to the one before (and now and then to an older one), so a constellation
    grows over three lines and starts fresh after. The word being sung gets a
    big star with a ring and its word in bold underneath; the newest line
    draws itself towards it. Star symbols: Sparkle, Star, Diamond, Orb, Cross,
    Heart, or Mixed (a different one on every word).
  - Bouncing: the sing-along bouncing ball. It hops from word to word in
    time with the song and lands exactly when the word is sung; the word it
    lands on springs up, sung words light up. Before the first word it
    bounces in place.

NEW IN 1.8
----------
  - Clock widget: Digital, Stacked, Minimal, Analog and Words ("twenty-five
    past twelve"). Font groups, weight, letter spacing, solid / gradient /
    visualizer colours, 24-hour, seconds, date in your Windows language,
    shadow, glow, pulse to the beat, left / centre / right.
  - Weather widget: search any city in the world, live weather from
    Open-Meteo (free, no account), refreshed every 15 minutes. Animated
    icons: the sun turns, clouds drift, rain and snow fall, lightning
    flashes, fog rolls, the moon and stars at night. Card, Minimal and
    Forecast (next four days), °C / °F.
  - Screen glow: light along the edges of your screens. 10 designs (Soft,
    Neon, Bars, LED strip, Breathe, Comets, Aurora, Oscilloscope, Embers,
    Prism), with the music or a steady breathing rhythm at your speed, all
    edges / bottom / sides / top and bottom, your colour or the album's or
    a rainbow, every screen or just the main one. It never takes a click
    and steps aside for full-screen games and video.
  - Tray menu: show / hide clock, weather and screen glow.

NEW IN 1.7
----------
  - Faded previous / next lines now only appear in Flow. Every other style
    shows just the line being sung:
      Typewriter, Cinematic, Glass, Karaoke - the line only
      Word reel - one big word; the last one flies up and away
      Subtitle  - the line with a thin bar showing how far it has been sung
      Stack     - a tower: one word per row, each new word drops on top
  - Scatter: words glide in out of a soft blur, shrink into their spot and
    settle with a little bounce, bright instead of just popping up.
  - 5 new styles: Rain (letters fall in one by one), Swing (words swing down
    on a hinge), Split (the line comes together from both sides), Focus (big
    and blurry, then sharp), Ink (hollow letters fill with ink as sung).

NEW IN 1.6
----------
  - Fixed for real: everything vanished after 20-40 seconds. Windows was handed
    memory addresses that Go could move or free (stack growth / garbage
    collection) - now every Windows call keeps its data pinned in place.
  - New settings window in the Weyv style: Widgets grid, a page per widget with
    live preview + summary on the right.
  - Every widget: On the desktop / Always on top, Small-Medium-Large-Huge,
    opacity, Lock (clicks go through), Reset position, Show me where it is.
  - Vinyl: 7 records (Picture disc, Black, Coloured, Splatter, Marble, Clear,
    Gold), colour from the theme / album / your own, 33 1/3 - 45 - 78 rpm,
    grooves that light up with the music, glow that breathes with the bass,
    needle that drops on and lifts off, click the record to play / pause.
  - Visualizer: 6 styles (Halo, Ring, Bars, Mirror, Blocks, Wave), 9 colour
    gradients incl. your own, backdrop, album art glow, now playing card.
  - Lyrics: Motion (Still, Pulse, Stretch, Glow, Dissolve), font groups
    (Modern, Condensed, Serif, Mono, Handwritten), As written / ALL CAPS /
    all lowercase.

NEW IN 1.5
----------
  - Fixed: a crash while drawing with many text sizes (Scatter / resizing).
  - 10 new lyric styles:
      Glass       - whole line dim, light sweeps through each word as it's sung
      Bounce      - words drop in and bounce
      Wave        - the line floats on a slow wave
      Glitch      - RGB split + jitter whenever a word lands
      Decode      - words scramble like a hacker screen and resolve as sung
      Highlighter - a marker colours the sung words
      Echo        - the current word leaves fading echoes behind it
      Orbit       - the line bends over an arc
      Slide       - words glide in from alternating sides
      Spotlight   - only the sung word is lit, the rest falls into shadow
  - 5 new effects: Chromatic, Long shadow, Hollow, Soft blur, Glass
  - 6 new colour themes: Black hole, Aurora, Vaporwave, Ocean, Crimson, Lavender
    (your Custom colours are kept)


START
-----
1. Put Orbit.exe somewhere (e.g. Documents\OrbitLyrics) and double-click it.
   "Windows protected your PC" (new, unsigned program): More info -> Run anyway.
2. The settings window opens the first time. Play a song on Spotify.
3. Later: CLICK the vinyl icon in the tray (bottom-right, maybe under ^) for the
   settings window, RIGHT-CLICK it for the quick menu.
4. Updating: just start the new Orbit.exe - it closes the old one by itself.


MOVING AND RESIZING WITH THE MOUSE
----------------------------------
  Arrange mode - turn it on by DOUBLE-CLICKING any widget on the desktop,
  pressing Ctrl+Alt+E, or from the tray menu / Settings -> Layout:
    - a box with corner handles appears around every widget
    - drag inside the box to move it anywhere
    - pull a corner, or scroll the mouse wheel over it, to resize
    - right-click a widget for the menu
    - click "Done" in the bar at the top of the screen (or Ctrl+Alt+E) to finish
  Outside arrange mode: scroll over a widget on the desktop to resize it.
  The record: with Scratch on, dragging the record itself turns it - move
  it by its song wheel / card, or in arrange mode.
  Also: tray (right-click) -> Move & resize, and Settings -> Layout sliders.


SETTINGS WINDOW
---------------
  Lyrics ........ style (12), random / fixed spot, every font installed on your PC
                  (with search), size, bold, ALL CAPS, previous line, line width,
                  effect (Soft glow, Strong glow, Shadow, Outline, Neon, Gradient)
                  + strength, colour themes or your own colours, timing offset.
                  Live preview at the top.
  Turntable ..... record style, corner (the list + arm flip on the right side),
                  spin speed, tonearm, track list, how many recent songs.
  Halo .......... bars, length, sensitivity, bass bounce, glow, slow spin.
  Layout ........ EDIT MODE: drag any widget, scroll over it to resize.
                  Reset buttons for each widget.
  Performance ... 0-60 frames per second. 0 = no animation (lowest CPU).
  General ....... start with Windows, settings/log folder, quit.

The settings window uses Microsoft Edge in "app mode" (no tabs, no address bar)
and talks to Orbit only on 127.0.0.1 with a secret key. Closing it doesn't
close Orbit.


RESOURCES
---------
Plain Windows drawing (GDI+), no browser engine running in the background.
Nothing is drawn while music is paused. Lower the FPS for even less CPU.


TROUBLESHOOTING
---------------
- Settings + log: %APPDATA%\OrbitLyrics (General -> Open). If something looks
  wrong, send orbit.log - and crash.log if Orbit closed by itself.
- No cover / "estimated sync": Windows media controls weren't readable, so the
  Spotify window title is used; lyrics start when the song changes and pause with
  it. Use the timing offset if needed.
- Halo doesn't move: check orbit.log for "audio:" lines.
- Widgets gone after Win+D: click the desktop or open any window.
