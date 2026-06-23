// Polyv (player.polyv.net / hls.videocc.net) helpers — used by Wangxiao233,
// Kaoyanvip, Magedu, Mashibing, Luffycity, Minshi, Kuke, and ~16 other sites.
//
// The polyv playback chain is:
//   1. https://player.polyv.net/secure/{vid}.json  — returns playsafe info
//   2. https://hls.videocc.net/{path1}/{path2}/{vid}_{bitrate}.m3u8  — manifest
//   3. https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//                                                  — AES-128 key per segment
//
// Some shops use the player.polyv.net/resp/vod-player-drm path for DRM content
// which adds a per-session JS-injected token (Mashibing); that's not supported
// without a JS sandbox.
package shared

const (
	PolyvSecurePlay      = "https://player.polyv.net/secure/{vid}.json"
	PolyvHLSManifestTmpl = "https://hls.videocc.net/%s/%s/%s_%s.m3u8"
	PolyvHLSKeyTmpl      = "https://hls.videocc.net/playsafe/%s/%s/%s_%s.key?token=%s"
	PolyvAPIVideoInfo    = "https://api.polyv.net/v2/video/{vid}/get-video-info"
	PolyvDRMLibJS        = "https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js"
)
