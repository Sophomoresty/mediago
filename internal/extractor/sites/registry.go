package sites

import (
	"github.com/nichuanfang/medigo/internal/extractor"
)

func init() {
	for _, s := range allSites {
		extractor.Register(
			newGenericExtractor(s.name, s.domain, s.patterns, s.needAuth, s.apiTemplate),
			extractor.SiteInfo{Name: s.name, URL: s.domain, NeedAuth: s.needAuth},
		)
	}
}

type siteSpec struct {
	name        string
	domain      string
	patterns    []string
	needAuth    bool
	apiTemplate string
}

var allSites = []siteSpec{
	{"Fenbi", "fenbi.com", []string{`fenbi\.com`}, true, ""},
	{"Huatu", "huatu.com", []string{`huatu\.com`}, true, ""},
	{"Gaodun", "gaodun.com", []string{`gaodun\.com`}, true, ""},
	{"Jianshe99", "jianshe99.com", []string{`jianshe99\.com`}, true, ""},
	{"Med66", "med66.com", []string{`med66\.com`}, true, ""},
	{"Hqwx", "hqwx.com", []string{`hqwx\.com`}, true, ""},
	{"Wangxiao", "wangxiao.cn", []string{`wangxiao\.cn`}, true, ""},
	{"Wangxiao233", "233.com", []string{`233\.com`}, true, ""},
	{"Dongao", "dongao.com", []string{`dongao\.com`}, true, ""},
	{"Eoffcn", "eoffcn.com", []string{`eoffcn\.com`}, true, ""},
	{"Kaoyanvip", "kaoyanvip.cn", []string{`kaoyanvip\.cn`}, true, ""},
	{"Yikaobang", "yikaobang.com", []string{`yikaobang\.com`}, true, ""},
	{"Smartedu", "smartedu.cn", []string{`smartedu\.cn`}, false, ""},
	{"Icourses", "icourses.cn", []string{`icourses\.cn`}, false, ""},
	{"Icve", "icve.com.cn", []string{`icve\.com\.cn`}, true, ""},
	{"Cnmooc", "cnmooc.org", []string{`cnmooc\.org`}, true, ""},
	{"Open163", "open.163.com", []string{`open\.163\.com`}, false, ""},
	{"Unipus", "unipus.cn", []string{`unipus\.cn`}, true, ""},
	{"Ahu", "elearning.ahu.edu.cn", []string{`ahu\.edu\.cn`}, true, ""},
	{"Nmkjxy", "nmkjxy.com", []string{`nmkjxy\.com`}, true, ""},
	{"Cto51", "51cto.com", []string{`51cto\.com`}, true, ""},
	{"Huke88", "huke88.com", []string{`huke88\.com`}, true, ""},
	{"Magedu", "magedu.com", []string{`magedu\.com`}, true, ""},
	{"Itbaizhan", "itbaizhan.com", []string{`itbaizhan\.com`}, true, ""},
	{"Luffycity", "luffycity.com", []string{`luffycity\.com`}, true, ""},
	{"Tmooc", "tmooc.cn", []string{`tmooc\.cn`}, true, ""},
	{"Mashibing", "mashibing.com", []string{`mashibing\.com`}, true, ""},
	{"Xueersi", "xueersi.com", []string{`xueersi\.com`}, true, ""},
	{"Yangcong", "yangcong345.com", []string{`yangcong345\.com`}, true, ""},
	{"Yixiaoerguo", "yixiaoerguo.com", []string{`yixiaoerguo\.com`}, true, ""},
	{"Speiyou", "speiyou.com", []string{`speiyou\.com`}, true, ""},
	{"Gaotu", "gaotu.cn", []string{`gaotu\.cn`}, true, ""},
	{"Koolearn", "koolearn.com", []string{`koolearn\.com`}, true, ""},
	{"Xiaoetech", "xiaoe-tech.com", []string{`xiaoe-tech\.com`}, true, ""},
	{"Xiaoeapp", "xiaoeknow.com", []string{`xiaoeknow\.com`}, true, ""},
	{"Youzan", "youzan.com", []string{`youzan\.com`}, true, ""},
	{"Qlchat", "qlchat.com", []string{`qlchat\.com`}, true, ""},
	{"Lizhiweike", "lizhiweike.com", []string{`lizhiweike\.com`}, true, ""},
	{"Renrenjiang", "renrenjiang.cn", []string{`renrenjiang\.cn`}, true, ""},
	{"Sanjieke", "sanjieke.cn", []string{`sanjieke\.cn`}, true, ""},
	{"Duanshu", "duanshu.com", []string{`duanshu\.com`}, true, ""},
	{"Lexueyun", "lexueyun.com", []string{`lexueyun\.com`}, true, ""},
	{"Meeting", "meeting.tencent.com", []string{`meeting\.tencent\.com`}, true, ""},
	{"Classin", "classin.com", []string{`classin\.com`}, true, ""},
	{"CCTalk", "cctalk.com", []string{`cctalk\.com`}, true, ""},
	{"Keqq", "ke.qq.com", []string{`ke\.qq\.com`}, true, ""},
	{"Baijiayunxiao", "baijiayun.com", []string{`baijiayun\.com`}, true, ""},
	{"Haozaixian", "haozaixian.cn", []string{`haozaixian\.cn`}, true, ""},
	{"Shanxiang", "shanxiang.org", []string{`shanxiang\.org`}, true, ""},
	{"Ledu", "ledu.com", []string{`ledu\.com`}, true, ""},
	{"Xiwang", "xiwang.com", []string{`xiwang\.com`}, true, ""},
	{"Xsteach", "xsteach.com", []string{`xsteach\.com`}, true, ""},
	{"Chaoge", "chaoge.com", []string{`chaoge\.com`}, true, ""},
	{"Ckjr", "ckjr.com", []string{`ckjr\.com`}, true, ""},
	{"Enetedu", "enetedu.com", []string{`enetedu\.com`}, true, ""},
	{"Wowtiku", "wowtiku.com", []string{`wowtiku\.com`}, true, ""},
	{"Haiyangknow", "haiyangknow.com", []string{`haiyangknow\.com`}, true, ""},
	{"Houda", "houda.com", []string{`houda\.com`}, true, ""},
	{"Houdu", "houdu.com", []string{`houdu\.com`}, true, ""},
	{"Htknow", "htknow.com", []string{`htknow\.com`}, true, ""},
	{"Jinbangshidai", "jinbangshidai.com", []string{`jinbangshidai\.com`}, true, ""},
	{"Jingtongxue", "jingtongxue.com", []string{`jingtongxue\.com`}, true, ""},
	{"Kaimingzhixue", "kaimingzhixue.com", []string{`kaimingzhixue\.com`}, true, ""},
	{"Kuke", "kuke.com", []string{`kuke\.com`}, true, ""},
	{"Mddclass", "mddclass.com", []string{`mddclass\.com`}, true, ""},
	{"Minshi", "minshi.com", []string{`minshi\.com`}, true, ""},
	{"Orangevip", "orangevip.com", []string{`orangevip\.com`}, true, ""},
	{"Plaso", "plaso.cn", []string{`plaso\.cn`}, true, ""},
	{"Qihang", "qihang.com", []string{`qihang\.com`}, true, ""},
	{"Sier", "sier.com", []string{`sier\.com`}, true, ""},
	{"Wallstreets", "wallstreets.com", []string{`wallstreets\.com`}, true, ""},
	{"Wendao", "wendao.com", []string{`wendao\.com`}, true, ""},
	{"Xuelang", "xuelang.com", []string{`xuelang\.com`}, true, ""},
	{"Yizhiknow", "yizhiknow.com", []string{`yizhiknow\.com`}, true, ""},
	{"Youdao", "youdao.com", []string{`youdao\.com`}, true, ""},
	{"Youyuan", "youyuan.com", []string{`youyuan\.com`}, true, ""},
	{"Zhaozhao", "zhaozhao.com", []string{`zhaozhao\.com`}, true, ""},
	{"Zhengbao", "zhengbao.com", []string{`zhengbao\.com`}, true, ""},
	{"Zlketang", "zlketang.com", []string{`zlketang\.com`}, true, ""},
	{"Aishangke", "aishangke.com", []string{`aishangke\.com`}, true, ""},
	{"Caixuetang", "caixuetang.com", []string{`caixuetang\.com`}, true, ""},
	{"Gongxuanwang", "gongxuanwang.com", []string{`gongxuanwang\.com`}, true, ""},
}
