//go:build darwin

package main

// The Windows test mode's timing marks (demo_windows.go); nothing to do here.

func secProf(name string) {}
func demoOn() bool        { return false }
