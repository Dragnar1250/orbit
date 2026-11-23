// orbit-sign: makes orbit-update.json for a release — the file every Orbit checks before it
// installs an update. Run it next to the new Orbit.exe:
//
//	orbit-sign.exe -key orbit-signing-key.txt -exe Orbit.exe -version 1.19.0 -notes "What's new"
//
// Then upload Orbit.exe AND orbit-update.json to the GitHub release named v1.19.0 (mark it latest).
// The key file is secret: never upload it anywhere.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const repo = "Dragnar1250/orbit"

func main() {
	keyFile := flag.String("key", "orbit-signing-key.txt", "the secret key file")
	exe := flag.String("exe", "Orbit.exe", "the new Orbit.exe")
	version := flag.String("version", "", "its version, e.g. 1.19.0")
	notes := flag.String("notes", "", "a line about what's new")
	out := flag.String("out", "orbit-update.json", "where to write the update file")
	flag.Parse()
	if *version == "" {
		fail("give -version (e.g. -version 1.19.0)")
	}
	kf, err := os.Open(*keyFile)
	if err != nil {
		fail("can't open the key file: " + err.Error())
	}
	var seed []byte
	sc := bufio.NewScanner(kf)
	for sc.Scan() {
		if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sc.Text())); err == nil && len(b) == ed25519.SeedSize {
			seed = b
		}
	}
	kf.Close()
	if seed == nil {
		fail("the key file doesn't hold a key")
	}
	key := ed25519.NewKeyFromSeed(seed)
	f, err := os.Open(*exe)
	if err != nil {
		fail("can't open " + *exe)
	}
	h := sha256.New()
	size, _ := io.Copy(h, f)
	f.Close()
	m := map[string]any{
		"version": *version,
		"url":     "https://github.com/" + repo + "/releases/download/v" + *version + "/Orbit.exe",
		"sha256":  hex.EncodeToString(h.Sum(nil)),
		"size":    size,
		"notes":   *notes,
	}
	text := "orbit-update\n" + *version + "\n" + m["url"].(string) + "\n" + m["sha256"].(string) + "\n" + strconv.FormatInt(size, 10)
	m["sig"] = base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(text)))
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Printf("%s written for %s %s (%d bytes)\npublic key: %s\n", *out, *exe, *version, size, base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
}

func fail(s string) {
	fmt.Fprintln(os.Stderr, "orbit-sign: "+s)
	os.Exit(1)
}
