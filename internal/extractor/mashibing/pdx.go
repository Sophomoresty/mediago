package mashibing

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// Polyv PDX encrypted playback helpers ported from Mashibing_Base.pyc.
//
// Constants (verbatim from source Mashibing_Base.pyc:41-53):
const (
	polyvSecureURL    = "https://player.polyv.net/secure/%s.json"
	polyvKeyURL       = "https://hls.videocc.net/playsafe/%s/%s/%s_%s.key?token=%s"
	polyvPDXLibPlayer = "https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js"
)

var (
	// polyv_pdx_secret from source line 43 (base64-decoded to 34 bytes, use first 32 for AES-256)
	polyvPDXSecret = b64DecodeOrPanic("OWtjN9xcDcc2cwXKxECpRgKw7piD4RwCdfOUlyNHFdSV0gHi=")
	// polyv_pdx_iv_bytes from source line 44-59
	polyvPDXIV = []byte{13, 22, 8, 12, 7, 6, 13, 1, 50, 11, 12, 8, 5, 16, 4, 1}
)

func b64DecodeOrPanic(s string) []byte {
	// Try standard first, then URL-safe
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			// Try without padding
			b, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
			if err != nil {
				panic(fmt.Sprintf("polyv PDX: cannot decode secret: %v", err))
			}
		}
	}
	// AES key must be 16, 24, or 32 bytes. Take first 32.
	if len(b) >= 32 {
		return b[:32]
	}
	padded := make([]byte, 32)
	copy(padded, b)
	return padded
}

// decryptPolyvPDXText decrypts a PDX-encrypted response body.
// Source _decrypt_polyv_pdx_text: base64 URL-safe decode → AES-CBC decrypt
// with polyv_pdx_secret key and polyv_pdx_iv_bytes IV.
func decryptPolyvPDXText(ciphertextB64 string) (string, error) {
	// URL-safe base64: replace - with + and _ with /
	ciphertextB64 = strings.NewReplacer("-", "+", "_", "/").Replace(ciphertextB64)
	// Add padding if needed
	if pad := len(ciphertextB64) % 4; pad > 0 {
		ciphertextB64 += strings.Repeat("=", 4-pad)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("polyv PDX base64 decode: %w", err)
	}

	block, err := aes.NewCipher(polyvPDXSecret)
	if err != nil {
		return "", fmt.Errorf("polyv PDX AES key: %w", err)
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("polyv PDX: ciphertext not block-aligned (%d)", len(ciphertext))
	}

	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, polyvPDXIV)
	mode.CryptBlocks(plaintext, ciphertext)

	// Strip PKCS7 padding
	pad := int(plaintext[len(plaintext)-1])
	if pad > 0 && pad <= aes.BlockSize && pad <= len(plaintext) {
		plaintext = plaintext[:len(plaintext)-pad]
	}

	return string(plaintext), nil
}

// decryptPolyvKey decrypts a polyv AES key response.
// Source _decrypt_polyv_key: AES-CBC decrypt with same secret/IV.
func decryptPolyvKey(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(polyvPDXSecret)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("polyv key: not block-aligned")
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, polyvPDXIV).CryptBlocks(plaintext, ciphertext)
	pad := int(plaintext[len(plaintext)-1])
	if pad > 0 && pad <= aes.BlockSize {
		plaintext = plaintext[:len(plaintext)-pad]
	}
	return plaintext, nil
}

// buildPolyvPDXKeyURL constructs the key URL for a PDX-encrypted segment.
// Source _build_polyv_pdx_key_url: /{vid}/{pid}/token={token}
func buildPolyvPDXKeyURL(vid, pid, token string) string {
	return fmt.Sprintf("%s/%s/%s/token=%s",
		strings.TrimSuffix("https://hls.videocc.net/playsafe", "/"),
		vid, pid, url.QueryEscape(token))
}

// buildPolyvPDXInfo assembles the PDX playback info struct.
// Source _build_polyv_pdx_info: creates { type, video_id, pdx_url, m3u8_url, pid, key_url, key_hex }
type polyvPDXInfo struct {
	VideoID string
	PDXURL  string
	M3U8URL string
	PID     string
	KeyURL  string
	KeyHex  string
}

// hexEncode returns uppercase hex string (for EXT-X-KEY inline format).
func hexEncode(b []byte) string {
	return strings.ToUpper(hex.EncodeToString(b))
}
