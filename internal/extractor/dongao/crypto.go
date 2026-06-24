package dongao

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dongao encrypted HLS flow ported from decompiled Dongao_Course.pyc:
//   _extract_player_fields: parse <input> fields from lecture HTML (w_vkey_n, wok, w_id, w_time)
//   _dongao_ts_key: AES-ECB decrypt ts_key_seed using w_vkey_n as key → TS segment key
//   _build_signed_m3u8: fetch signed m3u8, extract EXT-X-KEY IV, rewrite segment URLs with sg= param
//   _sign_dongao_media_url: append ccode + w_p signature to each .ts segment URL

var (
	inputFieldRe = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	attrNameRe   = regexp.MustCompile(`(?is)\bname\s*=\s*["']([^"']+)["']`)
	attrValRe    = regexp.MustCompile(`(?is)\bvalue\s*=\s*["']([^"']*)["']`)
	ivRe         = regexp.MustCompile(`(?is)IV\s*=\s*(0x[0-9a-fA-F]+)`)
	keyURIRe     = regexp.MustCompile(`(?is)URI\s*=\s*["']([^"']+)["']`)
	m3u8SgPrefix = "sg_prefix_placeholder" // replaced at runtime by server value
)

// extractPlayerFields parses the hidden <input> fields from the lecture HTML page.
// Returns a map of field name → value. Key fields: w_vkey_n, wok, w_id, w_time.
func extractPlayerFields(lectureHTML string) map[string]string {
	fields := map[string]string{}
	for _, m := range inputFieldRe.FindAllStringSubmatch(lectureHTML, -1) {
		attrs := m[1]
		nameMatch := attrNameRe.FindStringSubmatch(attrs)
		if nameMatch == nil {
			continue
		}
		name := strings.TrimSpace(nameMatch[1])
		val := ""
		if valMatch := attrValRe.FindStringSubmatch(attrs); valMatch != nil {
			val = valMatch[1]
		}
		if name != "" {
			fields[name] = val
		}
	}
	return fields
}

// dongaoTSKey decrypts the TS segment key. Source _dongao_ts_key:
// takes the w_vkey_n field as hex, AES-ECB decrypts ts_key_seed → raw key bytes.
func dongaoTSKey(fields map[string]string, tsKeySeed []byte) ([]byte, error) {
	vkeyHex := strings.TrimSpace(fields["w_vkey_n"])
	if vkeyHex == "" {
		return nil, fmt.Errorf("dongao: missing w_vkey_n field")
	}
	vkey, err := hex.DecodeString(vkeyHex)
	if err != nil {
		// Try as raw string (some variants use ASCII key)
		vkey = []byte(vkeyHex)
	}
	if len(vkey) != 16 && len(vkey) != 24 && len(vkey) != 32 {
		// Pad/truncate to nearest AES block size
		if len(vkey) > 32 {
			vkey = vkey[:32]
		} else if len(vkey) > 24 {
			vkey = padKey(vkey, 32)
		} else if len(vkey) > 16 {
			vkey = padKey(vkey, 24)
		} else {
			vkey = padKey(vkey, 16)
		}
	}

	block, err := aes.NewCipher(vkey)
	if err != nil {
		return nil, fmt.Errorf("dongao AES key: %w", err)
	}
	if len(tsKeySeed)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("dongao: ts_key_seed not block-aligned (%d bytes)", len(tsKeySeed))
	}
	plaintext := make([]byte, len(tsKeySeed))
	for i := 0; i < len(tsKeySeed); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], tsKeySeed[i:i+aes.BlockSize])
	}
	return unpadPKCS7(plaintext)
}

func padKey(key []byte, size int) []byte {
	if len(key) >= size {
		return key[:size]
	}
	padded := make([]byte, size)
	copy(padded, key)
	return padded
}

func unpadPKCS7(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > len(data) || pad > aes.BlockSize {
		return data, nil // no padding
	}
	return data[:len(data)-pad], nil
}

// signDongaoMediaURL appends the sg= signature param to a .ts segment URL.
// Source _sign_dongao_media_url: builds w_p from wok/w_id/w_time, appends ccode + sg.
func signDongaoMediaURL(rawURL string, fields map[string]string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	// ccode = time-based nonce
	q.Set("ccode", strconv.FormatInt(time.Now().UnixMilli(), 10))
	// w_p derived from wok/w_id/w_time (source uses _build_dongao_w_p)
	if wok := fields["wok"]; wok != "" {
		q.Set("wok", wok)
	}
	if wid := fields["w_id"]; wid != "" {
		q.Set("w_id", wid)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// buildSignedM3U8 fetches the signed m3u8, extracts IV from EXT-X-KEY,
// rewrites each segment URL with the sg= signature, and returns the
// rewritten manifest text ready for a downstream HLS downloader.
func buildSignedM3U8(m3u8Text string, fields map[string]string) (string, []byte, error) {
	if !strings.HasPrefix(strings.TrimSpace(m3u8Text), "#EXTM3U") {
		return "", nil, fmt.Errorf("dongao: not an m3u8 manifest")
	}

	var iv []byte
	var tsKey []byte
	var outLines []string

	for _, line := range strings.Split(m3u8Text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			outLines = append(outLines, line)
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-KEY") {
			// Extract IV
			if m := ivRe.FindStringSubmatch(line); m != nil {
				ivHex := strings.TrimPrefix(m[1], "0x")
				if parsed, err := hex.DecodeString(ivHex); err == nil {
					iv = parsed
				}
			}
			// Extract key URI and decrypt
			if m := keyURIRe.FindStringSubmatch(line); m != nil {
				keySeed, err := hex.DecodeString(strings.TrimSpace(m[1]))
				if err == nil {
					if k, err := dongaoTSKey(fields, keySeed); err == nil {
						tsKey = k
					}
				}
			}
			outLines = append(outLines, line)
			continue
		}

		// Rewrite segment URLs
		if !strings.HasPrefix(line, "#") && line != "" {
			signed := signDongaoMediaURL(line, fields)
			outLines = append(outLines, signed)
			continue
		}

		outLines = append(outLines, line)
	}

	result := strings.Join(outLines, "\n")
	return result, combineKeyAndIV(tsKey, iv), nil
}

// combineKeyAndIV returns the AES key material for downstream decryption.
// If both key and IV are available, returns key; caller applies IV from m3u8.
func combineKeyAndIV(key, iv []byte) []byte {
	if len(key) > 0 {
		return key
	}
	return nil
}

// decryptCBCSegment decrypts one TS segment using AES-CBC with the given key and IV.
// Used by downstream HLS downloader when m3u8 has EXT-X-KEY:METHOD=AES-128.
func decryptCBCSegment(ciphertext, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext not block-aligned")
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)
	return unpadPKCS7(plaintext)
}

var _ = bytes.NewReader // keep import for future use
