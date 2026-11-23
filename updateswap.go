package main

// The file side of installing an update — kept apart from Windows so every case can be tested:
//   swapIn:       Orbit.exe → Orbit.exe.old, the new file → Orbit.exe (put back if anything fails)
//   restoreOld:   the new one didn't start: Orbit.exe → Orbit.exe.bad, Orbit.exe.old → Orbit.exe
//   startCheck:   at every start — after a failed trial go back, after a good one tidy up

import (
	"fmt"
	"io"
	"os"
	"strings"
)

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

// swapIn puts newFile in place of exe. On any failure exe is left exactly as it was.
func swapIn(exe, newFile string) error {
	if _, err := os.Stat(newFile); err != nil {
		return fmt.Errorf("the update file is gone")
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("Orbit's folder can't be changed (is it in Program Files?). Move Orbit to another folder, or update by hand")
	}
	if err := copyFile(newFile, exe); err != nil {
		os.Remove(exe)
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("couldn't put the new version in place, and couldn't restore the old one (%v) — Orbit.exe.old is the old version", rerr)
		}
		return fmt.Errorf("couldn't put the new version in place: %v", err)
	}
	return nil
}

// restoreOld: back to the version before the update (the new one is kept as .bad for the log).
func restoreOld(exe string) error {
	old := exe + ".old"
	if _, err := os.Stat(old); err != nil {
		return fmt.Errorf("the old version is gone")
	}
	os.Remove(exe + ".bad")
	if err := os.Rename(exe, exe+".bad"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(old, exe); err != nil {
		os.Rename(exe+".bad", exe) // undo: leave things as they were
		return err
	}
	return nil
}

// startAction: what a starting Orbit has to do about updates.
type startAction int

const (
	startNormal     startAction = iota // nothing (old files tidied)
	startTrial                         // first run after an update: prove it works for 30 s
	startRolledBack                    // the last update never proved itself: the old one is back — start it, stop here
)

// startCheck: args are the command line; trial is the marker file a trial run leaves until it
// has run 30 s. start runs an exe (the restored old version).
func startCheck(exe, trial string, args []string, start func(string) error) (startAction, string) {
	for i, a := range args {
		if a == "--after-update" && i+1 < len(args) {
			from := args[i+1]
			os.WriteFile(trial, []byte(from), 0o644)
			return startTrial, from
		}
	}
	if b, err := os.ReadFile(trial); err == nil {
		os.Remove(trial)
		if _, err := os.Stat(exe + ".old"); err == nil {
			from := strings.TrimSpace(string(b))
			if restoreOld(exe) == nil && start(exe) == nil {
				return startRolledBack, from
			}
		}
	}
	os.Remove(exe + ".old") // left from an update that went fine
	return startNormal, ""
}
