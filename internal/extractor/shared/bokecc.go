// BokeCC (p.bokecc.com / cc.video) helpers — used by Qihang, Jingtongxue, and
// other sites embedding the BokeCC player.
//
// BokeCC playback chain:
//   https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}
//                                      — returns mp4/m3u8 fragment URLs
//
// `siteid` is the BokeCC tenant ID hardcoded by each parent site (Qihang uses
// `A183AC83A2983CCC`).
package shared

const (
	BokeCCGetVideoFile = "https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}"
)
