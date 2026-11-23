//go:build !windows && !darwin

package main

// Tests elsewhere: a stand-in "lock" (reversible, marked) so the config code paths still run.

func protectBytes(plain []byte) ([]byte, bool) {
	out := append([]byte("TEST:"), plain...)
	for i := 5; i < len(out); i++ {
		out[i] ^= 0x5A
	}
	return out, true
}

func unprotectBytes(sealed []byte) ([]byte, bool) {
	if len(sealed) < 5 || string(sealed[:5]) != "TEST:" {
		return nil, false
	}
	out := append([]byte(nil), sealed[5:]...)
	for i := range out {
		out[i] ^= 0x5A
	}
	return out, true
}
