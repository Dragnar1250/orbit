package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// updatedFrom: set when this run is the first one after an update (the old version).
var updatedFrom string

func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

func trialFile() string { return filepath.Join(appDir(), "update", "trial") }

// handleUpdateStart runs first thing. true: this run is over (it rolled back and started the
// old version).
func handleUpdateStart() bool {
	exe := exePath()
	if exe == "" {
		return false
	}
	old, bad := exe+".old", exe+".bad"
	for i, a := range os.Args {
		if a == "--after-update" && i+1 < len(os.Args) {
			updatedFrom = os.Args[i+1]
			_ = os.MkdirAll(filepath.Dir(trialFile()), 0o755)
			_ = os.WriteFile(trialFile(), []byte(updatedFrom), 0o644)
			return false
		}
	}
	if b, err := os.ReadFile(trialFile()); err == nil {
		if _, err := os.Stat(old); err == nil {
			// the new version was started after an update but never ran long enough to say it is
			// fine (it crashed): go back to the old one
			logf("update to %s didn't work out (it never ran 30 s) — going back to %s", appVersion, strings.TrimSpace(string(b)))
			os.Remove(bad)
			if os.Rename(exe, bad) == nil {
				if os.Rename(old, exe) == nil {
					os.Remove(trialFile())
					if exec.Command(exe).Start() == nil {
						return true
					}
				} else {
					os.Rename(bad, exe)
				}
			}
		}
		os.Remove(trialFile())
	}
	os.Remove(old) // left over from an update that went fine
	return false
}

// afterUpdateStarted: the new version is up — after 30 s it counts as working; the old exe goes.
func afterUpdateStarted() {
	if updatedFrom == "" {
		return
	}
	go func() {
		time.Sleep(30 * time.Second)
		os.Remove(trialFile())
		os.Remove(exePath() + ".old")
		os.RemoveAll(filepath.Join(appDir(), "update"))
		logf("update from %s to %s confirmed", updatedFrom, appVersion)
		runOnUI(func() {
			trayBalloon("Orbit is up to date", strings.NewReplacer("{v}", appVersion, "{old}", updatedFrom).Replace(T("Now on {v} (was {old}). What's new: Settings → System.")))
		})
	}()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// installUpdate: swap the exe (a running exe can be renamed on Windows), start the new one — it
// closes this one as it starts. If it doesn't within 25 s, the old exe is put back.
func installUpdate() {
	path, ver := updater.readyFile()
	if path == "" || fileSHA(path) == "" {
		return
	}
	exe := exePath()
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		logf("update: can't replace %s: %v", exe, err)
		updater.fail("Orbit's folder can't be changed (is it in Program Files?). Move Orbit to another folder, or update by hand.")
		trayBalloon("Update not installed", "Orbit's folder can't be changed. Settings → System has the details.")
		return
	}
	if err := copyFile(path, exe); err != nil {
		os.Remove(exe)
		os.Rename(old, exe)
		logf("update: copy failed: %v", err)
		updater.fail("Couldn't put the new version in place: " + err.Error())
		return
	}
	saveConfigIfDue(true)
	closeFullscreen()
	cmd := exec.Command(exe, "--after-update", appVersion)
	if err := cmd.Start(); err != nil {
		os.Remove(exe)
		os.Rename(old, exe)
		updater.fail("Couldn't start the new version")
		return
	}
	logf("update: started %s, waiting for it to take over", ver)
	go func() {
		time.Sleep(25 * time.Second)
		// still here: the new one never closed us — it failed to start
		cmd.Process.Kill()
		time.Sleep(500 * time.Millisecond)
		os.Remove(exe + ".bad")
		os.Rename(exe, exe+".bad")
		os.Rename(old, exe)
		os.Remove(trialFile())
		logf("update: %s didn't start — kept %s", ver, appVersion)
		updater.fail("The new version (" + ver + ") didn't start, so Orbit stayed on " + appVersion + ".")
		runOnUI(func() {
			trayBalloon("Update didn't work", strings.Replace(T("Orbit stayed on {v}."), "{v}", appVersion, 1))
		})
	}()
}

// updateLoop: look for updates a little after start, then every 6 hours. With automatic
// installing on, a ready update goes in when full screen isn't open.
func updateLoop() {
	time.Sleep(90 * time.Second)
	for {
		var check, auto bool
		withCfg(func() { check, auto = cfg.UpdateCheck, cfg.UpdateAuto })
		if check {
			updater.check(true, func() { runOnUI(func() { updateReady(auto) }) })
		}
		time.Sleep(6 * time.Hour)
	}
}

var updateWaiting bool // ready, waiting for full screen to close

func updateReady(auto bool) {
	_, ver := updater.readyFile()
	if ver == "" {
		return
	}
	if !auto {
		trayBalloon(strings.Replace(T("Orbit {v} is ready"), "{v}", ver, 1), "Settings → System → Update now (takes a second).")
		return
	}
	if fs != nil {
		updateWaiting = true
		return
	}
	installUpdate()
}

// updateTick: called every second; installs a waiting update once full screen is closed.
func updateTick() {
	if updateWaiting && fs == nil && !editModeFlag.Load() {
		updateWaiting = false
		installUpdate()
	}
}
