package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
	_ = os.MkdirAll(filepath.Dir(trialFile()), 0o755)
	act, from := startCheck(exe, trialFile(), os.Args, func(p string) error { return exec.Command(p).Start() })
	switch act {
	case startTrial:
		updatedFrom = from
	case startRolledBack:
		logf("update to %s didn't work out (it never ran 30 s) — went back to %s", appVersion, from)
		return true
	}
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
			trayBalloon("Orbit is up to date", "Now on "+appVersion+" (was "+updatedFrom+"). What's new: Settings → System.")
		})
	}()
}

// installUpdate: swap the exe (a running exe can be renamed on Windows), start the new one — it
// closes this one as it starts. If it doesn't within 25 s, the old exe is put back.
func installUpdate() {
	path, ver := updater.readyFile()
	if path == "" || fileSHA(path) == "" {
		return
	}
	exe := exePath()
	if err := swapIn(exe, path); err != nil {
		logf("update: %v", err)
		updater.fail(err.Error())
		trayBalloon("Update not installed", "Settings → System has the details.")
		return
	}
	saveConfigIfDue(true)
	closeFullscreen()
	cmd := exec.Command(exe, "--after-update", appVersion)
	if err := cmd.Start(); err != nil {
		restoreOld(exe)
		updater.fail("Couldn't start the new version")
		return
	}
	logf("update: started %s, waiting for it to take over", ver)
	go func() {
		time.Sleep(25 * time.Second)
		// still here: the new one never closed us — it failed to start
		cmd.Process.Kill()
		time.Sleep(500 * time.Millisecond)
		restoreOld(exe)
		os.Remove(trialFile())
		logf("update: %s didn't start — kept %s", ver, appVersion)
		updater.fail("The new version (" + ver + ") didn't start, so Orbit stayed on " + appVersion + ".")
		runOnUI(func() { trayBalloon("Update didn't work", "Orbit stayed on "+appVersion+".") })
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
		trayBalloon("Orbit "+ver+" is ready", "Settings → System → Update now (takes a second).")
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
