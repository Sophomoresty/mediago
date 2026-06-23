// Package shared contains helpers for third-party video platforms (CSSL Cloud,
// Polyv, BokeCC, Baijiayun) embedded inside multiple parent sites.
//
// Why a shared module: at least 38 of the upstream sites embed one of these
// four CDN/player platforms for actual video delivery. The parent site only
// performs authentication and returns CDN tokens; the manifest/segment URLs
// always come from the shared platform. Centralizing the URL construction
// keeps per-site extractors small.
//
// This file holds CSSLcloud helpers. The full signed-token playback flow
// requires the parent site's session, but the public CDN endpoints are
// documented here for downstream extractors.
package shared

import (
	"fmt"
)

// CSSLcloud public CDN endpoints from decompiled Mooc/Courses/{Houda,Jianshe99,
// Med66,Qihang,Shanxiang,Aishangke,Chaoge}/<site>_Course.pyc.
const (
	CssLcloudReplayLogin    = "https://view.csslcloud.net/replay/user/login"
	CssLcloudReplayMeta     = "https://view.csslcloud.net/replay/data/meta"
	CssLcloudReplayPlay     = "https://view.csslcloud.net/replay/video/play"
	CssLcloudAPIRecordVod   = "https://view.csslcloud.net/api/record/vod?accountId={user_id}&recordId={record_id}&terminal=3&token={token}"
	CssLcloudAPIRoomReplay  = "https://view.csslcloud.net/api/room/replay/login?roomid={room_id}&userid={user_id}&recordid={record_id}&viewertoken={uid}%3A{lid}"
)

// CssLcloudReplayPlayURL builds a URL for the replay/video/play endpoint with
// the standard query params. The exact tokens needed (uid, lid, ts, sign)
// come from a per-site login flow that's not implemented yet.
func CssLcloudReplayPlayURL(uid, lid, ts, sign string) string {
	return fmt.Sprintf("%s?uid=%s&lid=%s&ts=%s&sign=%s", CssLcloudReplayPlay, uid, lid, ts, sign)
}
