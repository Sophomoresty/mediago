package imooc

import (
	"strings"
	"testing"
)

func TestDecryptInfoBasic(t *testing.T) {
	// The test data from Imooc_Free.pyc test2(): a known encoded string
	// that should decode to a valid HLS m3u8 manifest (starts with #EXTM3U).
	// We cannot verify exact output without the runtime, but we verify the
	// function does not panic and produces non-empty output for well-formed input.
	encoded := "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"

	result, err := decryptInfo(encoded)
	if err != nil {
		t.Fatalf("decryptInfo failed: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("decryptInfo returned empty result")
	}
	// The decoded string should contain HLS-related content (URLs or m3u8 markers).
	text := string(result)
	if !strings.Contains(text, "imooc.com") && !strings.Contains(text, "#EXTM3U") && !strings.Contains(text, "http") {
		t.Errorf("decryptInfo output does not look like HLS content: %s", text[:min(200, len(text))])
	}
}

func TestDecryptInfoKeyDecode(t *testing.T) {
	// Test the key decode path from imooc_key_decode:
	// The test data from Imooc_Config test:
	// 'd1SqmhCkFVRvVCKvVDzuUVRcU1RVVOiTVCb2shTfsDYwReoft8RPZQkmGRw7'
	// This should decode via the JSON wrapper to a 16-byte key.
	encoded := "d1SqmhCkFVRvVCKvVDzuUVRcU1RVVOiTVCb2shTfsDYwReoft8RPZQkmGRw7"

	// Wrap in JSON like the original does.
	result, err := decryptInfo(encoded)
	if err != nil {
		t.Fatalf("decryptInfo key decode failed: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("decryptInfo key decode returned empty result")
	}
	t.Logf("key decode result (%d bytes): %x", len(result), result)
}

func TestDecryptInfoTooShort(t *testing.T) {
	_, err := decryptInfo("abc")
	if err == nil {
		t.Fatal("expected error for too-short data")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
