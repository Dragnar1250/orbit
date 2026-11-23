ORBIT 1.18  -  desktop music widgets for Windows 10 / 11
=============================================================

Sits on your desktop (behind your windows, on top of any wallpaper - Wallpaper
Engine, Lively or a normal picture) and shows:

  - a spinning turntable in a corner: picture disc (the cover IS the record) or a
    vinyl with the cover as label, tonearm that moves in as the song plays,
    now playing + recently played on an arc around it
  - synced lyrics that pop up in a RANDOM spot for every line (or a fixed spot)
    in 29 styles: Flow, Word reel, Cinematic, Kinetic, Typewriter, Neon, Subtitle,
    Stack, Rise, Pop, Karaoke, Scatter, Glass, Bounce, Wave, Glitch, Decode,
    Highlighter, Echo, Orbit, Slide, Spotlight, Rain, Swing, Split, Focus, Ink, Constellation, Bouncing
  - 12 text effects (glows, shadow, outline, neon, gradient, chromatic RGB split,
    long 3D shadow, hollow, soft blur, frosted glass) and 14 colour themes
  - a halo visualizer: the album cover with bars that react to the music

Works with Spotify and anything in the Windows media controls. Lyrics: lrclib.net


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
