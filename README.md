# Orbit

Free desktop music widgets for Windows 10 / 11 — a Weyv-like spinning turntable, synced lyrics in
29 styles, a music-reactive halo, clock, weather, screen glow, and a full-screen mode (music display
or a whole "desktop skin" with your Spotify queue, search, shuffle and music videos).

**Download:** [latest release](https://github.com/Dragnar1250/orbit/releases/latest) → `Orbit.exe`
(or the zip). Orbit updates itself after that; every update is signed and checked before it's installed.

The user guide is in [README.txt](README.txt).

## Building

Go 1.24, no other dependencies (Windows APIs are called directly):

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o Orbit.exe .
go test -race ./...
```

Every push is built and tested automatically (Linux + real Windows); pushing a tag `vX.Y.Z` builds,
signs and publishes the release.
