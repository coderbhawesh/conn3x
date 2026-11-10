// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package eos

import (
	"crypto/sha512"
	"strings"
)

const cryptAlphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// SecretHash is password as EOS stores a secret: SHA-512 crypt with the
// default 5000 rounds, "$6$<salt>$<hash>", the form "secret sha512" takes.
// With it the password itself never reaches the switch's CLI, and a read can
// tell which secret an account holds. salt is up to 16 characters of the
// crypt alphabet.
func SecretHash(password, salt string) string {
	p, s := []byte(password), []byte(salt)
	sum := func(parts ...[]byte) []byte {
		h := sha512.New()
		for _, part := range parts {
			h.Write(part)
		}
		return h.Sum(nil)
	}
	// fill is n bytes of digest, repeated as needed.
	fill := func(digest []byte, n int) []byte {
		return []byte(strings.Repeat(string(digest), n/len(digest)+1))[:n]
	}
	b := sum(p, s, p)
	a := sha512.New()
	a.Write(p)
	a.Write(s)
	a.Write(fill(b, len(p)))
	for i := len(p); i > 0; i >>= 1 {
		if i&1 != 0 {
			a.Write(b)
		} else {
			a.Write(p)
		}
	}
	prev := a.Sum(nil)
	dp := fill(sum([]byte(strings.Repeat(password, len(p)))), len(p))
	ds := fill(sum([]byte(strings.Repeat(salt, 16+int(prev[0])))), len(s))
	for r := 0; r < 5000; r++ {
		var parts [][]byte
		if r&1 != 0 {
			parts = append(parts, dp)
		} else {
			parts = append(parts, prev)
		}
		if r%3 != 0 {
			parts = append(parts, ds)
		}
		if r%7 != 0 {
			parts = append(parts, dp)
		}
		if r&1 != 0 {
			parts = append(parts, prev)
		} else {
			parts = append(parts, dp)
		}
		prev = sum(parts...)
	}
	out := []byte("$6$" + salt + "$")
	emit := func(w uint32, n int) {
		for ; n > 0; n-- {
			out = append(out, cryptAlphabet[w&0x3f])
			w >>= 6
		}
	}
	for i, x := 0, [3]int{0, 21, 42}; i < 21; i++ {
		emit(uint32(prev[x[0]])<<16|uint32(prev[x[1]])<<8|uint32(prev[x[2]]), 4)
		x = [3]int{x[1] + 1, x[2] + 1, x[0] + 1}
	}
	emit(uint32(prev[63]), 2)
	return string(out)
}

// CryptSalt turns seed, 12 bytes or more, into a 16-character crypt salt.
func CryptSalt(seed []byte) string {
	var b strings.Builder
	for i := 0; i < 12; i += 3 {
		w := uint32(seed[i])<<16 | uint32(seed[i+1])<<8 | uint32(seed[i+2])
		for n := 0; n < 4; n++ {
			b.WriteByte(cryptAlphabet[w&0x3f])
			w >>= 6
		}
	}
	return b.String()
}
