package main

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// nibbleAlpha is the 16-character lookup table used to encode each nibble of
// the hash byte into a character. The alphabet was chosen by oh-my-pi to avoid
// overlap with decimal digits (0-9) and standard hex (a-f), making LINE#HASH
// anchors unambiguous to parse even when embedded in model output.
const nibbleAlpha = "ZPMQVRWSNKTXJBYH"

// nibble converts a 4-bit value (0–15) to the corresponding alphabet character.
func nibble(v byte) byte {
	return nibbleAlpha[v&0x0f]
}

// computeLineHash returns a 2-character hash tag for a single file line.
//
// Algorithm:
//  1. Strip trailing CR and trailing whitespace (normalise line endings).
//  2. If the line contains no letter or digit (blank lines, punctuation-only
//     lines like "{" or "}"), mix the 1-indexed line number into the FNV32a
//     state before hashing, so structurally identical lines at different
//     positions don't collide.
//  3. Compute FNV-1a 32-bit hash of the normalised bytes.
//  4. Take the lowest byte of the hash and split it into two 4-bit nibbles,
//     each mapped through nibbleAlpha.
//
// The result is always exactly 2 characters from nibbleAlpha.
func computeLineHash(lineNum int, line string) string {
	// Strip trailing CR and whitespace
	line = strings.TrimRight(line, "\r")
	line = strings.TrimRightFunc(line, unicode.IsSpace)

	h := fnv.New32a()

	// Mix in the line number for lines without any significant character so
	// that e.g. two consecutive blank lines get different hashes.
	hasSig := false
	for _, r := range line {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasSig = true
			break
		}
	}
	if !hasSig {
		// Write the line number as a varint-style seed before the content.
		n := lineNum
		for n > 0 {
			h.Write([]byte{byte(n & 0xff)})
			n >>= 8
		}
	}

	h.Write([]byte(line))
	sum := h.Sum32()
	lo := byte(sum & 0xff)
	return string([]byte{nibble(lo >> 4), nibble(lo)})
}

// formatTag formats a line reference as "LINE#HASH" (e.g. "5#WS").
func formatTag(lineNum int, line string) string {
	return intToStr(lineNum) + "#" + computeLineHash(lineNum, line)
}

// ────────────────────────────────────────────────────────────────────────────
// Integer formatting without fmt (avoids import cycle in tests)
// ────────────────────────────────────────────────────────────────────────────

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
