// Baijiayun (baijiayun.com/vod, api.baijiayun.com) helpers — used by
// Baijiayunxiao, Jinbangshidai, Kaimingzhixue, Orangevip, and Youyuan.
//
// Baijiayun has two playback flows:
//
//   Live replay (web/playback):
//     https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id}&token={token}&use_encrypt=0&render=jsonp
//
//   VOD playback:
//     https://www.baijiayun.com/vod/video/getPlayUrl?vid={video_id}&render=jsonp&token={token}&use_encrypt=0
//
// Both endpoints accept JSONP (callback wrapped) and require the parent site
// to provide a per-room or per-video token signed against the tenant's app key.
package shared

const (
	BaijiayunGetPlayInfo = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id}&token={token}&use_encrypt=0&render=jsonp"
	BaijiayunGetPlayURL  = "https://www.baijiayun.com/vod/video/getPlayUrl?vid={video_id}&render=jsonp&token={token}&use_encrypt=0"
)
