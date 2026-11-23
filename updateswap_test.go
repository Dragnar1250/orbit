package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func setupExe(t *testing.T) (dir, exe, upd, trial string) {
	dir = t.TempDir()
	exe = filepath.Join(dir, "Orbit.exe")
	upd = filepath.Join(dir, "Orbit-1.19.0.exe")
	trial = filepath.Join(dir, "trial")
	os.WriteFile(exe, []byte("OLD 1.18"), 0o755)
	os.WriteFile(upd, []byte("NEW 1.19"), 0o755)
	return
}

// a whole good update: swap, first start is a trial, the next start tidies up
func TestUpdateHappyPath(t *testing.T) {
	_, exe, upd, trial := setupExe(t)
	if err := swapIn(exe, upd); err != nil {
		t.Fatal(err)
	}
	if read(t, exe) != "NEW 1.19" || read(t, exe+".old") != "OLD 1.18" {
		t.Fatal("swap")
	}
	act, from := startCheck(exe, trial, []string{"Orbit.exe", "--after-update", "1.18.0"}, nil)
	if act != startTrial || from != "1.18.0" || !exists(trial) {
		t.Fatal("trial")
	}
	// it ran 30 s: marker gone (afterUpdateStarted does that); next start just tidies
	os.Remove(trial)
	act, _ = startCheck(exe, trial, []string{"Orbit.exe"}, func(string) error { t.Fatal("must not start anything"); return nil })
	if act != startNormal || exists(exe+".old") || read(t, exe) != "NEW 1.19" {
		t.Fatal("tidy")
	}
}

// the new version crashed during its trial: the next start (of the new exe) goes back by itself
func TestUpdateCrashedTrialRollsBack(t *testing.T) {
	_, exe, upd, trial := setupExe(t)
	swapIn(exe, upd)
	startCheck(exe, trial, []string{"Orbit.exe", "--after-update", "1.18.0"}, nil) // … and it crashes
	started := ""
	act, from := startCheck(exe, trial, []string{"Orbit.exe"}, func(p string) error { started = p; return nil })
	if act != startRolledBack || from != "1.18.0" {
		t.Fatalf("act %v", act)
	}
	if read(t, exe) != "OLD 1.18" || read(t, exe+".bad") != "NEW 1.19" || started != exe || exists(trial) || exists(exe+".old") {
		t.Fatalf("rollback: exe=%q bad=%q started=%q", read(t, exe), read(t, exe+".bad"), started)
	}
	// and the old one starts normally afterwards (no loop)
	act, _ = startCheck(exe, trial, []string{"Orbit.exe"}, nil)
	if act != startNormal || read(t, exe) != "OLD 1.18" {
		t.Fatal("after rollback")
	}
}

// a trial marker but no old exe (someone deleted it): nothing to go back to — just run
func TestUpdateTrialWithoutOld(t *testing.T) {
	_, exe, upd, trial := setupExe(t)
	swapIn(exe, upd)
	os.Remove(exe + ".old")
	os.WriteFile(trial, []byte("1.18.0"), 0o644)
	act, _ := startCheck(exe, trial, []string{"Orbit.exe"}, func(string) error { t.Fatal("started"); return nil })
	if act != startNormal || read(t, exe) != "NEW 1.19" || exists(trial) {
		t.Fatal("no old")
	}
}

// rolling back but the old exe can't be started: stay on the new one rather than nothing
func TestUpdateRollbackStartFails(t *testing.T) {
	_, exe, upd, trial := setupExe(t)
	swapIn(exe, upd)
	os.WriteFile(trial, []byte("1.18.0"), 0o644)
	act, _ := startCheck(exe, trial, []string{"Orbit.exe"}, func(string) error { return errors.New("no") })
	if act != startNormal {
		t.Fatal("must carry on when the old one can't start")
	}
	if !exists(exe) {
		t.Fatal("no Orbit.exe left")
	}
}

// "--after-update" without a version, or odd arguments: a normal start
func TestUpdateOddArgs(t *testing.T) {
	_, exe, _, trial := setupExe(t)
	for _, args := range [][]string{{"Orbit.exe", "--after-update"}, {"Orbit.exe", "--probe"}, {}, {"--after-update"}} {
		if act, _ := startCheck(exe, trial, args, nil); act != startNormal || exists(trial) {
			t.Fatalf("%v → %v", args, act)
		}
	}
}

// the update file vanished before installing: nothing is touched
func TestUpdateSwapMissingFile(t *testing.T) {
	_, exe, upd, _ := setupExe(t)
	os.Remove(upd)
	if swapIn(exe, upd) == nil || read(t, exe) != "OLD 1.18" || exists(exe+".old") {
		t.Fatal("missing update file")
	}
}

// a folder Orbit can't write to (Program Files): refused, Orbit.exe untouched
func TestUpdateSwapReadOnlyFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	dir, exe, upd, _ := setupExe(t)
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	if swapIn(exe, upd) == nil || read(t, exe) != "OLD 1.18" {
		t.Fatal("read-only folder")
	}
}

// the copy fails half way (target can't be created): the old exe is put back
func TestUpdateSwapCopyFails(t *testing.T) {
	_, exe, _, _ := setupExe(t)
	dirAsFile := filepath.Join(t.TempDir(), "notafile")
	os.Mkdir(dirAsFile, 0o755) // reading a directory as the new exe fails mid-copy
	if swapIn(exe, dirAsFile) == nil {
		t.Fatal("copy of a directory accepted")
	}
	if read(t, exe) != "OLD 1.18" || exists(exe+".old") {
		t.Fatalf("not restored: %q old=%v", read(t, exe), exists(exe+".old"))
	}
}

// a leftover .old from earlier and a leftover .bad: both handled
func TestUpdateLeftovers(t *testing.T) {
	_, exe, upd, trial := setupExe(t)
	os.WriteFile(exe+".old", []byte("ANCIENT"), 0o755)
	os.WriteFile(exe+".bad", []byte("ANCIENT BAD"), 0o755)
	if err := swapIn(exe, upd); err != nil || read(t, exe+".old") != "OLD 1.18" {
		t.Fatal("old leftover")
	}
	if err := restoreOld(exe); err != nil || read(t, exe) != "OLD 1.18" || read(t, exe+".bad") != "NEW 1.19" {
		t.Fatalf("restore over a leftover .bad: %v", err)
	}
	if restoreOld(exe) == nil {
		t.Fatal("restore twice must fail (nothing to restore)")
	}
	if read(t, exe) != "OLD 1.18" {
		t.Fatal("second restore broke it")
	}
	_ = trial
}
