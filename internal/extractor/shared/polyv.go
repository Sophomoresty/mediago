// Polyv (player.polyv.net / hls.videocc.net) helpers — used by Mashibing,
// Wangxiao233, Kaoyanvip, Magedu, Luffycity, Minshi, Kuke, Gongxuanwang,
// Jinbangshidai, Plaso, Youyuan, Orangevip, Zhaozhao (~16 sites).
//
// Non-DRM polyv playback chain ported from Mashibing_Base.pyc constants:
//  1. GET  https://player.polyv.net/secure/{vid}.json
//     → returns { code: 200, data: { playsafe: { token }, paths: [...], dur } }
//  2. Manifest URL: https://hls.videocc.net/{path1}/{path2}/{vid}_{bitrate}.m3u8
//     (path1/path2 derived from vid; bitrate from polyv's quality picker)
//  3. EXT-X-KEY URI in manifest must be re-fetched with the playsafe token:
//     https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//
// PDX/DRM polyv (the `vod-player-drm/canary/next/lib_player.js` flow used by
// premium courses) returns an encrypted PDX envelope. The helpers below decrypt
// the envelope, resolve tokenized v13 key URLs, and expose downloader metadata.
package shared

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/util"
)

// Polyv endpoints (verbatim from source).
const (
	PolyvSecureURLTmpl  = "https://player.polyv.net/secure/%s.json"
	PolyvHLSPlayBase    = "https://hls.videocc.net"
	PolyvDRMLibPlayerJS = "https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js"

	polyvPDXSecretV2     = "OWtjN9xcDcc2cwXKxECpRgKw7piD4RwCdfOUlyNHFdSV0gHi="
	polyvPDXSecretLegacy = "NTQ1ZjhmY2QtMzk3OS00NWZhLTkxNjktYzk3NTlhNDNhNTQ4#"
)

var (
	polyvPDXIVV2     = []byte{13, 22, 8, 12, 7, 6, 13, 1, 50, 11, 12, 8, 5, 16, 4, 1}
	polyvPDXIVLegacy = []byte{1, 1, 2, 3, 5, 8, 13, 21, 34, 21, 13, 8, 5, 3, 2, 1}
)

// PolyvSecure is the response from player.polyv.net/secure/{vid}.json.
type PolyvSecure struct {
	Code          int      `json:"code"`
	Status        string   `json:"status"`
	Message       string   `json:"message"`
	HLS           []string `json:"hls"`
	Body          string   `json:"body"`
	Title         string   `json:"title"`
	Token         string   `json:"token"`
	PlaySafe      string   `json:"playSafe"`
	PlaySafeToken string   `json:"playSafeToken"`
	Playsafe      struct {
		Token string `json:"token"`
	} `json:"playsafe"`
	// TopSeedConst is the seed_const returned by older Polyv secure payloads.
	TopSeedConst any  `json:"seed_const"`
	Encrypted    bool `json:"encrypted"`
	Data         struct {
		Playsafe struct {
			Token string `json:"token"`
		} `json:"playsafe"`
		// Paths is the per-quality m3u8 list. Each entry contains a path
		// fragment (path1/path2) and a quality marker.
		Paths     []string `json:"paths"`
		HLS       []string `json:"hls"`
		Body      string   `json:"body"`
		Dur       int      `json:"dur"`
		Title     string   `json:"title"`
		SeedConst any      `json:"seed_const"`
		Encrypted bool     `json:"encrypted"`
	} `json:"data"`
}

// PolyvRewriteOptions controls key URL construction and key decryption.
type PolyvRewriteOptions struct {
	Token       string
	Referer     string
	ManifestURL string
	SeedConst   string
}

// PolyvPDXOptions controls PDX/DRM envelope resolution.
type PolyvPDXOptions struct {
	VideoID       string
	PDXURL        string
	PlaySafeToken string
	Headers       map[string]string
	Secure        *PolyvSecure
	PID           string
}

// PolyvPDXInfo is the resolved PDX manifest/key metadata required by the HLS
// downloader. M3U8Text is already absolutized and its EXT-X-KEY URI points at
// the tokenized /playsafe/v13/ endpoint.
type PolyvPDXInfo struct {
	Type             string
	VideoID          string
	PDXURL           string
	M3U8URL          string
	M3U8Text         string
	PID              string
	KeyURL           string
	KeyHex           string
	IVHex            string
	SeedConst        string
	PlaySafeToken    string
	PDXVersion       string
	Segments         []string
	SegmentDurations []float64
}

// PolyvResolveSecure fetches secure/{vid}.json and returns the parsed envelope.
// Parent site provides cookies/headers via the *util.Client.
func PolyvResolveSecure(c *util.Client, vid string, headers map[string]string) (*PolyvSecure, error) {
	if c == nil {
		return nil, fmt.Errorf("polyv: nil client")
	}
	vid = PolyvNormalizeVID(vid)
	if vid == "" {
		return nil, fmt.Errorf("polyv: empty vid")
	}
	apiURL := fmt.Sprintf(PolyvSecureURLTmpl, url.PathEscape(vid))
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("polyv secure: %w", err)
	}
	var sec PolyvSecure
	if err := json.Unmarshal([]byte(body), &sec); err != nil {
		return nil, fmt.Errorf("polyv secure parse: %w (body=%q)", err, truncate(body, 200))
	}
	if sec.Code != 200 && sec.Code != 0 {
		return nil, fmt.Errorf("polyv secure: code=%d message=%q", sec.Code, sec.Message)
	}
	if !sec.hasManifestCandidates() {
		if decrypted, err := decryptPolyvSecureEnvelope(vid, firstNonEmptyString(sec.Body, sec.Data.Body)); err == nil {
			sec.mergeLegacyEnvelope(decrypted)
		}
	}
	return &sec, nil
}

// PolyvPickBestManifest returns the highest-quality manifest URL from a
// secure response. Polyv's encrypted flag can accompany the legacy secure
// body/key flow that the Python source still resolves, so manifest selection
// must be driven by playable HLS/path candidates instead of blocking on that
// flag alone.
func PolyvPickBestManifest(sec *PolyvSecure) (string, error) {
	if sec == nil {
		return "", fmt.Errorf("polyv: nil secure response")
	}
	// Paths are typically ordered highest-quality first.
	for _, p := range sec.Data.Paths {
		if strings.TrimSpace(p) != "" {
			return PolyvNormalizeManifestURL(p), nil
		}
	}
	if p, ok := pickLastNonEmpty(sec.Data.HLS); ok {
		return PolyvNormalizeManifestURL(p), nil
	}
	if p, ok := pickLastNonEmpty(sec.HLS); ok {
		return PolyvNormalizeManifestURL(p), nil
	}
	return "", fmt.Errorf("polyv: no playable paths in secure response")
}

// PolyvRewriteM3U8Keys rewrites EXT-X-KEY URI entries. Plain/decrypted
// 16-byte keys are inlined as 0x{hex}; encrypted keys that cannot be decrypted
// fall back to the tokenized key URL, matching the Python downloader behavior.
func PolyvRewriteM3U8Keys(c *util.Client, m3u8Text, token, referer string) (string, error) {
	return PolyvRewriteM3U8KeysWithOptions(c, m3u8Text, PolyvRewriteOptions{Token: token, Referer: referer})
}

// PolyvResolvePDX fetches and decrypts a Polyv PDX envelope, then fetches the
// tokenized v13 key so callers can pass both m3u8 text and cryptor metadata to
// the downloader.
func PolyvResolvePDX(c *util.Client, opts PolyvPDXOptions) (*PolyvPDXInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("polyv pdx: nil client")
	}
	pdxURL := PolyvNormalizeManifestURL(opts.PDXURL)
	if pdxURL == "" {
		return nil, fmt.Errorf("polyv pdx: empty pdx URL")
	}
	token := firstNonEmptyString(opts.PlaySafeToken, opts.Secure.PlayToken())
	if token == "" {
		return nil, fmt.Errorf("polyv pdx: empty playsafe token")
	}
	pid := strings.TrimSpace(opts.PID)
	if pid == "" {
		pid = buildPolyvPDXPID()
	}
	m3u8URL := addPolyvURLQueryParams(pdxURL, map[string]string{
		"pid":    pid,
		"device": "desktop",
		"token":  token,
	})
	body, err := c.GetString(m3u8URL, opts.Headers)
	if err != nil {
		return nil, fmt.Errorf("polyv pdx fetch: %w", err)
	}
	m3u8Text, seed, version, err := PolyvDecryptPDXText(body, opts.Secure.SeedConst())
	if err != nil {
		return nil, err
	}
	if !strings.Contains(m3u8Text, "#EXTM3U") {
		return nil, fmt.Errorf("polyv pdx: decrypted body is not m3u8")
	}
	m3u8Text = polyvAbsolutizeM3U8URLs(m3u8Text, m3u8URL)
	keyURL := PolyvBuildPDXKeyURL(extractM3U8URIFromText(m3u8Text), m3u8URL, token, pid)
	if keyURL == "" {
		return nil, fmt.Errorf("polyv pdx: empty key URL")
	}
	keyBytes, err := c.GetBytes(keyURL, opts.Headers)
	if err != nil {
		return nil, fmt.Errorf("polyv pdx key fetch: %w", err)
	}
	if len(keyBytes) == 0 {
		return nil, fmt.Errorf("polyv pdx: empty key body")
	}
	keyHex := strings.ToUpper(hex.EncodeToString(keyBytes))
	m3u8Text = replaceFirstM3U8URI(m3u8Text, keyURL)
	return &PolyvPDXInfo{
		Type:             "pdx",
		VideoID:          opts.VideoID,
		PDXURL:           pdxURL,
		M3U8URL:          m3u8URL,
		M3U8Text:         m3u8Text,
		PID:              pid,
		KeyURL:           keyURL,
		KeyHex:           keyHex,
		IVHex:            extractM3U8IVHex(m3u8Text),
		SeedConst:        seed,
		PlaySafeToken:    token,
		PDXVersion:       version,
		Segments:         extractPolyvM3U8Segments(m3u8Text),
		SegmentDurations: extractPolyvM3U8SegmentDurations(m3u8Text),
	}, nil
}

// ExtraMap returns the metadata shape used by site extractors' Extra fields.
func (info *PolyvPDXInfo) ExtraMap() map[string]any {
	if info == nil {
		return map[string]any{}
	}
	return map[string]any{
		"type":              firstNonEmptyString(info.Type, "pdx"),
		"video_id":          info.VideoID,
		"pdx_url":           info.PDXURL,
		"m3u8_url":          info.M3U8URL,
		"m3u8_text":         info.M3U8Text,
		"pid":               info.PID,
		"key_url":           info.KeyURL,
		"key_hex":           info.KeyHex,
		"iv_hex":            info.IVHex,
		"seed_const":        info.SeedConst,
		"play_safe_token":   info.PlaySafeToken,
		"pdx_version":       info.PDXVersion,
		"segments":          info.Segments,
		"segment_durations": info.SegmentDurations,
		"_lel_cryptor_type": "polyv_pdx",
	}
}

// M3U8Meta returns downloader metadata compatible with the source Python
// _build_polyv_pdx_m3u8_meta contract.
func (info *PolyvPDXInfo) M3U8Meta() map[string]any {
	if info == nil {
		return map[string]any{}
	}
	cryptor := info.polyvPDXCryptorMeta()
	return map[string]any{
		"cryptor":      cryptor,
		"_lel_cryptor": cryptor,
	}
}

func (info *PolyvPDXInfo) polyvPDXCryptorMeta() map[string]any {
	segmentDurations := append([]float64(nil), info.SegmentDurations...)
	return map[string]any{
		"type":                "polyv_pdx",
		"crf":                 24,
		"preset":              "ultrafast",
		"decode_batch_frames": 30,
		"fps_probe_frames":    1,
		"fps":                 "20",
		"segment_durations":   segmentDurations,
		"segments":            append([]string(nil), info.Segments...),
		"chunk_size":          polyvPDXChunkSize(len(info.Segments)),
		"ffmpeg_path":         "ffmpeg",
		"decoder_js_path":     "",
		"lib_player_path":     "",
		"iv_hex":              info.IVHex,
		"key_hex":             info.KeyHex,
		"key_url":             info.KeyURL,
		"pdx_version":         firstNonEmptyString(info.PDXVersion, "2"),
		"seed_const":          info.SeedConst,
		"play_safe_token":     info.PlaySafeToken,
		"pid":                 info.PID,
		"pdx_url":             info.PDXURL,
		"m3u8_url":            info.M3U8URL,
		"_lel_cryptor_type":   "polyv_pdx",
	}
}

func polyvPDXChunkSize(segmentCount int) int {
	return 1
}

// PolyvRewriteM3U8KeysWithOptions rewrites EXT-X-KEY URI entries to inline
// hex keys when possible, resolving relative Polyv key URIs against the
// manifest URL and decrypting 32-byte Polyv protected key payloads when
// seed_const is present.
func PolyvRewriteM3U8KeysWithOptions(c *util.Client, m3u8Text string, opts PolyvRewriteOptions) (string, error) {
	if c == nil {
		return "", fmt.Errorf("polyv: nil client")
	}
	if !strings.HasPrefix(strings.TrimSpace(m3u8Text), "#EXTM3U") {
		return "", fmt.Errorf("polyv: input is not an m3u8 manifest")
	}
	headers := map[string]string{}
	if opts.Referer != "" {
		headers["Referer"] = opts.Referer
	}

	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(m3u8Text, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "#EXT-X-KEY") {
			out = append(out, line)
			continue
		}
		uri := extractM3U8URI(line)
		if uri == "" {
			out = append(out, line)
			continue
		}
		keyURL := polyvKeyURL(uri, opts.ManifestURL, opts.Token)
		keyBytes, err := c.GetBytes(keyURL, headers)
		if err != nil {
			return "", fmt.Errorf("polyv key fetch %s: %w", keyURL, err)
		}
		replacement := keyURL
		if key, ok := PolyvDecryptKey(keyBytes, opts.SeedConst); ok {
			replacement = "0x" + strings.ToUpper(encodeHex(key))
		} else if len(keyBytes) == 16 {
			replacement = "0x" + strings.ToUpper(encodeHex(keyBytes))
		}
		out = append(out, strings.ReplaceAll(line, uri, replacement))
	}
	return strings.Join(out, "\n"), nil
}

// PolyvNormalizeVID converts bare Polyv ids to the secure endpoint form
// observed in the Python source, e.g. "abc123" -> "abc123_a".
func PolyvNormalizeVID(vid string) string {
	vid = strings.TrimSpace(vid)
	if vid == "" {
		return ""
	}
	if strings.Contains(vid, "_") {
		base := strings.SplitN(vid, "_", 2)[0]
		if base == "" || len(base) == 0 {
			return vid
		}
		return base + "_" + base[:1]
	}
	return vid + "_" + vid[:1]
}

// PolyvNormalizeManifestURL turns secure payload path fragments into absolute
// hls.videocc.net URLs.
func PolyvNormalizeManifestURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return strings.TrimRight(PolyvHLSPlayBase, "/") + "/" + strings.TrimLeft(raw, "/")
}

// PlayToken returns the playsafe token regardless of whether the secure
// endpoint used the new data envelope or legacy top-level fields.
func (sec *PolyvSecure) PlayToken() string {
	if sec == nil {
		return ""
	}
	return firstNonEmptyString(sec.Data.Playsafe.Token, sec.Playsafe.Token, sec.PlaySafeToken, sec.PlaySafe, sec.Token)
}

// SeedConst returns Polyv seed_const as a string for key decryption.
func (sec *PolyvSecure) SeedConst() string {
	if sec == nil {
		return ""
	}
	return firstNonEmptyString(polyvAnyString(sec.Data.SeedConst), polyvAnyString(sec.TopSeedConst))
}

// PolyvDecryptKey decrypts Polyv's 32-byte protected AES key body using
// seed_const. It returns false for plain 16-byte keys or unavailable seeds.
func PolyvDecryptKey(keyBytes []byte, seedConst string) ([]byte, bool) {
	seedConst = strings.TrimSpace(seedConst)
	if len(keyBytes) != 32 || seedConst == "" {
		return nil, false
	}
	seeds := []string{seedConst}
	for i := 0; i < 1000; i++ {
		s := fmt.Sprint(i)
		if s != seedConst {
			seeds = append(seeds, s)
		}
	}
	for _, seed := range seeds {
		key, ok := polyvDecryptKeyWithSeed(keyBytes, seed)
		if ok {
			return key, true
		}
	}
	return nil, false
}

// PolyvDecryptPDXText decrypts a PDX envelope body. Polyv v2 uses
// md5(polyv_pdx_secret + seed)[1:17] as the AES-CBC key; older envelopes use a
// legacy secret/IV pair. seedFallback is used when the envelope omits seed_const.
func PolyvDecryptPDXText(pdxText, seedFallback string) (string, string, string, error) {
	pdxText = strings.TrimSpace(pdxText)
	if pdxText == "" {
		return "", "", "", fmt.Errorf("polyv pdx: empty envelope")
	}
	var envelope map[string]any
	body := ""
	seed := ""
	version := "2"
	if err := json.Unmarshal([]byte(pdxText), &envelope); err == nil {
		body = firstNonEmptyString(polyvAnyString(envelope["body"]), polyvAnyString(envelope["data"]))
		seed = firstNonEmptyString(polyvAnyString(envelope["seed_const"]), polyvAnyString(envelope["seedConst"]), polyvAnyString(envelope["seed"]))
		version = firstNonEmptyString(polyvAnyString(envelope["version"]), version)
	} else {
		body = pdxText
	}
	seed = normalizePolyvPDXSeed(firstNonEmptyString(seed, seedFallback))
	if body == "" || seed == "" {
		return "", "", "", fmt.Errorf("polyv pdx: missing body or seed")
	}
	secret, iv := polyvPDXSecretV2, polyvPDXIVV2
	if strings.TrimSpace(version) != "2" {
		secret, iv = polyvPDXSecretLegacy, polyvPDXIVLegacy
	}
	keySeed := md5.Sum([]byte(secret + seed))
	key := []byte(hex.EncodeToString(keySeed[:])[1:17])
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", "", fmt.Errorf("polyv pdx AES key: %w", err)
	}
	ciphertext, err := safePolyvB64Decode(body)
	if err != nil {
		return "", "", "", fmt.Errorf("polyv pdx base64: %w", err)
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", "", "", fmt.Errorf("polyv pdx: ciphertext not block aligned")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	if unpadded, ok := stripPolyvPKCS7(plain); ok {
		plain = unpadded
	}
	return strings.TrimSpace(string(plain)), seed, strings.TrimSpace(version), nil
}

// PolyvBuildPDXKeyURL rewrites a PDX EXT-X-KEY URI to Polyv's tokenized v13
// playsafe key endpoint.
func PolyvBuildPDXKeyURL(keyURL, m3u8URL, playSafeToken, pid string) string {
	keyURL = polyvResolveAgainst(keyURL, m3u8URL)
	if keyURL == "" || playSafeToken == "" {
		return ""
	}
	u, err := url.Parse(keyURL)
	if err != nil {
		return ""
	}
	path := u.Path
	if strings.Contains(path, "/playsafe/") {
		path = regexp.MustCompile(`/playsafe/(?:v\d+/)?`).ReplaceAllString(path, "/playsafe/v13/")
	} else if strings.HasPrefix(path, "/") {
		path = "/playsafe/v13" + path
	} else {
		path = "/playsafe/v13/" + path
	}
	q := u.Query()
	q.Set("token", playSafeToken)
	if pid != "" {
		q.Set("pid", pid)
	}
	u.Path = path
	u.RawQuery = q.Encode()
	return u.String()
}

func buildPolyvPDXPID() string {
	now := time.Now()
	return fmt.Sprintf("%dX%07d", now.UnixMilli(), now.Nanosecond()%10000000)
}

func addPolyvURLQueryParams(raw string, params map[string]string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return raw
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func normalizePolyvPDXSeed(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	if f, err := strconv.ParseFloat(seed, 64); err == nil {
		return fmt.Sprintf("%d", int64(f))
	}
	return seed
}

func polyvAbsolutizeM3U8URLs(text, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil || base == nil || base.Scheme == "" || base.Host == "" {
		return text
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#EXT-X-KEY") {
			if uri := extractM3U8URI(trimmed); uri != "" {
				lines[i] = strings.Replace(line, uri, polyvResolveAgainst(uri, base.String()), 1)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines[i] = strings.Replace(line, trimmed, polyvResolveAgainst(trimmed, base.String()), 1)
	}
	return strings.Join(lines, "\n")
}

func extractM3U8URIFromText(text string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-KEY") {
			if uri := extractM3U8URI(line); uri != "" {
				return uri
			}
		}
	}
	return ""
}

func replaceFirstM3U8URI(text, replacement string) string {
	if replacement == "" {
		return text
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-KEY") {
			continue
		}
		if uri := extractM3U8URI(line); uri != "" {
			lines[i] = strings.Replace(line, uri, replacement, 1)
			return strings.Join(lines, "\n")
		}
	}
	return text
}

func extractM3U8IVHex(text string) string {
	re := regexp.MustCompile(`(?i)\bIV\s*=\s*0x([0-9a-f]+)`)
	if m := re.FindStringSubmatch(text); len(m) == 2 {
		return strings.ToUpper(m[1])
	}
	return ""
}

func extractPolyvM3U8Segments(text string) []string {
	var out []string
	expectSegment := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#EXTINF") {
			expectSegment = true
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if expectSegment || strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") {
			out = append(out, trimmed)
		}
		expectSegment = false
	}
	return out
}

func extractPolyvM3U8SegmentDurations(text string) []float64 {
	var out []float64
	re := regexp.MustCompile(`(?i)#EXTINF\s*:\s*([0-9.]+)`)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(trimmed), "#EXTINF") {
			continue
		}
		duration := 0.0
		if m := re.FindStringSubmatch(trimmed); len(m) == 2 {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				duration = f
			}
		}
		out = append(out, duration)
	}
	return out
}

func polyvDecryptKeyWithSeed(keyBytes []byte, seed string) ([]byte, bool) {
	sum := md5.Sum([]byte(seed))
	aesKey := []byte(hex.EncodeToString(sum[:])[:16])
	iv, err := hex.DecodeString("01020305070B0D1113171D0705030201")
	if err != nil {
		return nil, false
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil || len(keyBytes)%aes.BlockSize != 0 {
		return nil, false
	}
	plain := make([]byte, len(keyBytes))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, keyBytes)
	if unpadded, ok := stripPolyvPKCS7(plain); ok && len(unpadded) == 16 {
		return unpadded, true
	}
	if len(plain) == 16 {
		return plain, true
	}
	return nil, false
}

func stripPolyvPKCS7(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return data, false
	}
	pad := int(data[len(data)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(data) {
		return data, false
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return data, false
		}
	}
	return data[:len(data)-pad], true
}

func polyvKeyURL(uri, manifestURL, token string) string {
	rawURI := strings.TrimSpace(uri)
	isRelative := rawURI != "" &&
		!strings.HasPrefix(rawURI, "http://") &&
		!strings.HasPrefix(rawURI, "https://") &&
		!strings.HasPrefix(rawURI, "//")
	keyURL := rawURI
	if manifestURL != "" {
		keyURL = polyvResolveAgainst(keyURL, manifestURL)
	} else if strings.HasPrefix(keyURL, "//") {
		keyURL = "https:" + keyURL
	}
	u, err := url.Parse(keyURL)
	if err != nil || !u.IsAbs() {
		return appendQueryToken(keyURL, token)
	}
	if token != "" {
		if (isRelative || strings.Contains(strings.ToLower(u.Host), "videocc.net")) && !strings.Contains(u.Path, "/playsafe/") {
			u.Path = "/playsafe" + ensureLeadingSlash(u.Path)
		}
		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func appendQueryToken(raw, token string) string {
	if token == "" || strings.Contains(raw, "token=") {
		return raw
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + "token=" + url.QueryEscape(token)
}

func polyvResolveAgainst(raw, baseRaw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "0x") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	base, err := url.Parse(baseRaw)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func pickLastNonEmpty(values []string) (string, bool) {
	for i := len(values) - 1; i >= 0; i-- {
		if v := strings.TrimSpace(values[i]); v != "" {
			return v, true
		}
	}
	return "", false
}

func (sec *PolyvSecure) hasManifestCandidates() bool {
	return sec != nil && (len(sec.Data.Paths) > 0 || len(sec.Data.HLS) > 0 || len(sec.HLS) > 0)
}

func (sec *PolyvSecure) mergeLegacyEnvelope(m map[string]any) {
	if sec == nil || len(m) == 0 {
		return
	}
	if len(sec.HLS) == 0 {
		sec.HLS = polyvStringList(m["hls"])
	}
	if sec.TopSeedConst == nil {
		sec.TopSeedConst = m["seed_const"]
	}
	if sec.Title == "" {
		sec.Title = polyvAnyString(m["title"])
	}
	if sec.Data.Title == "" {
		sec.Data.Title = sec.Title
	}
	if sec.Token == "" {
		sec.Token = polyvAnyString(m["token"])
	}
	if sec.PlaySafe == "" {
		sec.PlaySafe = firstNonEmptyString(polyvAnyString(m["playSafe"]), polyvAnyString(m["playsafe"]), polyvAnyString(m["play_safe"]))
	}
	if sec.PlaySafeToken == "" {
		sec.PlaySafeToken = firstNonEmptyString(polyvAnyString(m["playSafeToken"]), polyvAnyString(m["playToken"]), polyvAnyString(m["play_token"]))
	}
	if sec.Playsafe.Token == "" {
		if nested, ok := m["playsafe"].(map[string]any); ok {
			sec.Playsafe.Token = polyvAnyString(nested["token"])
		}
	}
}

func decryptPolyvSecureEnvelope(vid, body string) (map[string]any, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("polyv secure: empty encrypted body")
	}
	ciphertext, err := hex.DecodeString(regexp.MustCompile(`[^0-9a-fA-F]`).ReplaceAllString(body, ""))
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("polyv secure: body is not AES block aligned")
	}
	sum := md5.Sum([]byte(PolyvNormalizeVID(vid)))
	hexSum := hex.EncodeToString(sum[:])
	block, err := aes.NewCipher([]byte(hexSum[:16]))
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, []byte(hexSum[16:32])).CryptBlocks(plain, ciphertext)
	candidates := [][]byte{plain, bytes.TrimRight(plain, "\x00")}
	if unpadded, ok := stripPolyvPKCS7(plain); ok {
		candidates = append(candidates, unpadded)
	}
	for _, candidate := range candidates {
		candidate = bytes.TrimSpace(candidate)
		if len(candidate) == 0 {
			continue
		}
		if decoded, err := safePolyvB64Decode(string(candidate)); err == nil {
			if out := parsePolyvJSONObject(decoded); out != nil {
				return out, nil
			}
		}
		if out := parsePolyvJSONObject(candidate); out != nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("polyv secure: decrypted body is not JSON")
}

func safePolyvB64Decode(text string) ([]byte, error) {
	text = strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(text))
	text = regexp.MustCompile(`[^A-Za-z0-9+/=]`).ReplaceAllString(text, "")
	text += strings.Repeat("=", (4-len(text)%4)%4)
	return base64.StdEncoding.DecodeString(text)
}

func parsePolyvJSONObject(data []byte) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func polyvAnyString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", x), "0"), ".")
	case int:
		return fmt.Sprint(x)
	case int64:
		return fmt.Sprint(x)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

func polyvStringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s := polyvAnyString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func encodeHex(b []byte) string {
	const digits = "0123456789ABCDEF"
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, c := range b {
		sb.WriteByte(digits[c>>4])
		sb.WriteByte(digits[c&0x0f])
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
