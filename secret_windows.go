package main

// Secrets in config.json (the Spotify sign-in) are locked with Windows' own DPAPI: only this
// Windows user on this PC can open them — a copied config.json is useless elsewhere.

import (
	"syscall"
	"unsafe"
)

var (
	crypt32             = syscall.NewLazyDLL("crypt32.dll")
	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	pLocalFreeSecret    = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
	secretEntropy       = []byte("Orbit config secret v1")
)

type dataBlob struct {
	n uint32
	p *byte
}

func blobOf(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{n: uint32(len(b)), p: &b[0]}
}

func (b *dataBlob) bytes() []byte {
	if b.p == nil || b.n == 0 {
		return nil
	}
	out := make([]byte, b.n)
	copy(out, unsafe.Slice(b.p, b.n))
	return out
}

const cryptprotectUIForbidden = 0x1

func protectBytes(plain []byte) ([]byte, bool) {
	var out dataBlob
	r, _, _ := pCryptProtectData.Call(uintptr(unsafe.Pointer(blobOf(plain))), 0, uintptr(unsafe.Pointer(blobOf(secretEntropy))),
		0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, false
	}
	defer pLocalFreeSecret.Call(uintptr(unsafe.Pointer(out.p)))
	return out.bytes(), true
}

func unprotectBytes(sealed []byte) ([]byte, bool) {
	var out dataBlob
	r, _, _ := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(blobOf(sealed))), 0, uintptr(unsafe.Pointer(blobOf(secretEntropy))),
		0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, false
	}
	defer pLocalFreeSecret.Call(uintptr(unsafe.Pointer(out.p)))
	return out.bytes(), true
}
