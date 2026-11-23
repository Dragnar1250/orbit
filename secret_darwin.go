//go:build darwin

package main

// On the Mac, secrets in config.json are sealed with AES-GCM under a random key kept in the
// user's Keychain ("Orbit config key"): a copied config.json is useless without that Keychain.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"strings"
	"sync"
)

var (
	macKeyOnce sync.Once
	macKey     []byte
)

func keychainKey() []byte {
	macKeyOnce.Do(func() {
		out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", "Orbit config key", "-a", "orbit", "-w").Output()
		if err == nil {
			if k, err := hex.DecodeString(strings.TrimSpace(string(out))); err == nil && len(k) == 32 {
				macKey = k
				return
			}
		}
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return
		}
		if exec.Command("/usr/bin/security", "add-generic-password", "-U", "-s", "Orbit config key", "-a", "orbit", "-w", hex.EncodeToString(k)).Run() == nil {
			macKey = k
		}
	})
	return macKey
}

func protectBytes(plain []byte) ([]byte, bool) {
	k := keychainKey()
	if k == nil {
		return nil, false
	}
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, false
	}
	return append(nonce, g.Seal(nil, nonce, plain, []byte("orbit"))...), true
}

func unprotectBytes(sealed []byte) ([]byte, bool) {
	k := keychainKey()
	if k == nil {
		return nil, false
	}
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	if len(sealed) < g.NonceSize() {
		return nil, false
	}
	p, err := g.Open(nil, sealed[:g.NonceSize()], sealed[g.NonceSize():], []byte("orbit"))
	return p, err == nil
}
