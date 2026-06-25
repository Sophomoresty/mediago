package imooc

// imooc_decode: pure-Go port of the imooc HLS payload decryption.
//
// The imooc API returns JSON with a `data.info` field containing an encoded
// string. The encoding applies a chain of reversible transforms (q/h/m/k)
// layered on top of standard base64.
//
// Algorithm (ported from xwz-dl Python implementation which itself was
// reverse-engineered from the obfuscated JS `imooc_decode` function):
//
//  1. Extract a 4-element transform table from the last 4 chars of the data.
//  2. Extract 12-char keys for transforms that need them (q, k).
//  3. Base64-decode the remaining payload.
//  4. Apply each transform in table order to get the plaintext bytes.

import (
	"encoding/base64"
	"fmt"
)

// decryptInfo decodes an imooc "info" payload string, returning the
// raw plaintext bytes (typically UTF-8 HLS manifest or 16-byte AES key).
func decryptInfo(data string) ([]byte, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("imooc decode: data too short (%d bytes)", len(data))
	}

	runes := []rune(data)

	// Step 1: compute position offsets from the last 4 chars (reversed order).
	// The positions are computed ONCE from the original tail, but the data
	// string is mutated (chars removed) between each extraction.
	// Important: the tail chars are NOT removed from the data; they remain
	// and may end up as part of extracted keys later.
	tail := runes[len(runes)-4:]
	positions := make([]int, 4)
	for i := range tail {
		positions[i] = int(tail[3-i]) % 4 // reversed order
	}

	// For each position, extract the char at (pos + 1) from the current
	// data, then remove that position from the data.
	transforms := make([]rune, 4)
	for i, pos := range positions {
		idx := pos + 1
		if idx >= len(runes) {
			return nil, fmt.Errorf("imooc decode: position %d out of range (len=%d)", idx, len(runes))
		}
		transforms[i] = runes[idx]
		runes = append(runes[:idx], runes[idx+1:]...)
	}

	// Step 2: extract 12-char keys from the end of data for transforms
	// that use them (q, k). Keys are collected in forward scan order,
	// reversed, then consumed via pop (from end) during transform
	// application. Net effect: each keyed transform gets its key in the
	// original collection order but consumed LIFO.
	var keys []string
	for _, item := range transforms {
		if item == 'q' || item == 'k' {
			if len(runes) < 12 {
				return nil, fmt.Errorf("imooc decode: not enough data to extract key (len=%d)", len(runes))
			}
			keys = append(keys, string(runes[len(runes)-12:]))
			runes = runes[:len(runes)-12]
		}
	}
	// The Python code does keys.reverse() then keys.pop() (LIFO).
	// After reverse + pop, the first pop returns the LAST element of
	// the reversed list, which is the FIRST collected key.
	// Effectively: keys are used in the same order they were collected.
	// No reverse needed; just use them in order.

	// Step 3: base64 decode the remaining data.
	decoded, err := base64.StdEncoding.DecodeString(string(runes))
	if err != nil {
		return nil, fmt.Errorf("imooc decode: base64 error: %w", err)
	}

	// Step 4: apply transforms in order.
	result := make([]int, len(decoded))
	for i, b := range decoded {
		result[i] = int(b)
	}

	keyIdx := 0
	for _, item := range transforms {
		switch item {
		case 'q':
			if keyIdx >= len(keys) {
				return nil, fmt.Errorf("imooc decode: not enough keys for transform q")
			}
			result = transformQ(result, keys[keyIdx])
			keyIdx++
		case 'h':
			result = transformH(result)
		case 'm':
			result = transformM(result)
		case 'k':
			if keyIdx >= len(keys) {
				return nil, fmt.Errorf("imooc decode: not enough keys for transform k")
			}
			result = transformK(result, keys[keyIdx])
			keyIdx++
		default:
			return nil, fmt.Errorf("imooc decode: unknown transform '%c'", item)
		}
	}

	out := make([]byte, len(result))
	for i, v := range result {
		out[i] = byte(v & 0xFF)
	}
	return out, nil
}

// transformQ: XOR each byte with the corresponding key byte (cycling).
func transformQ(data []int, key string) []int {
	kb := []byte(key)
	out := make([]int, len(data))
	for i, v := range data {
		out[i] = v ^ int(kb[i%len(kb)])
	}
	return out
}

// transformH: conditional swap based on value % 3.
func transformH(data []int) []int {
	out := make([]int, len(data))
	copy(out, data)
	i := 0
	for i < len(out) {
		offset := out[i] % 3
		if offset != 0 && i+offset < len(out) {
			out[i+1], out[i+offset] = out[i+offset], out[i+1]
			i = i + offset + 1
		}
		i++
	}
	return out
}

// transformM: remove padding bytes inserted after odd values.
func transformM(data []int) []int {
	// First pass: count output elements.
	count := 0
	i := 0
	for i < len(data) {
		if data[i]%2 != 0 {
			i++ // skip the padding byte after odd value
		}
		count++
		i++
	}

	out := make([]int, count)
	ii := 0
	oi := 0
	for ii < len(data) {
		out[oi] = data[ii]
		if data[ii]%2 != 0 {
			ii++ // skip padding
		}
		oi++
		ii++
	}
	return out
}

// transformK: swap + XOR with key.
func transformK(data []int, key string) []int {
	kb := []byte(key)
	out := make([]int, len(data))
	copy(out, data)

	i := 0
	for i < len(out) {
		offset := out[i] % 5
		if offset%5 != 0 && offset != 1 && i+offset < len(out) {
			out[i+1], out[i+offset] = out[i+offset], out[i+1]
			xorIdx := i + 2
			endIdx := i + offset + 1
			for endIdx-2 > xorIdx {
				out[xorIdx] ^= int(kb[xorIdx%len(kb)])
				xorIdx++
			}
			i = endIdx
		}
		i++
	}

	for i, v := range out {
		out[i] = v ^ int(kb[i%len(kb)])
	}
	return out
}
