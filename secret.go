package main

import (
	"encoding/base64"
	"strings"
)

const sealedPrefix = "dpapi:"

// sealSecret: what is written to config.json for a secret ("" stays "").
func sealSecret(s string) string {
	if s == "" {
		return ""
	}
	b, ok := protectBytes([]byte(s))
	if !ok {
		return "" // never fall back to writing it in the clear
	}
	return sealedPrefix + base64.StdEncoding.EncodeToString(b)
}

// openSecret: the secret from config.json. A sealed one that this user/PC can't open (copied from
// elsewhere) gives "". 1.17 wrote it in the clear: taken as is (sealed at the next save).
func openSecret(s string) string {
	if !strings.HasPrefix(s, sealedPrefix) {
		return s
	}
	b, err := base64.StdEncoding.DecodeString(s[len(sealedPrefix):])
	if err != nil {
		return ""
	}
	p, ok := unprotectBytes(b)
	if !ok {
		return ""
	}
	return string(p)
}
