package mddclass

import (
	"fmt"
	"net/url"
)

func (sess *mddclassSession) webHeaders(domain, referer, accept string) map[string]string {
	companyHost := sess.companyHost(domain)
	if referer == "" {
		referer = companyHost + "/"
	}
	if accept == "" {
		accept = "application/json, text/plain, */*"
	}
	headers := map[string]string{
		"User-Agent":      mddclassUserAgent,
		"Accept":          accept,
		"Referer":         referer,
		"Origin":          companyHost,
		"Hujiang-App-Key": mddclassPCWebKey,
		"SKsight-App-Key": mddclassPCWebKey,
		"X-CC-COMPANY":    mddclassFirstText(domain, sess.CompanyDomain, mddclassCompanyDomain),
		"Cookie":          sess.Cookie,
		"cookie":          sess.Cookie,
	}
	sess.applyAuthHeaders(headers)
	return headers
}

func (sess *mddclassSession) pcContentHeaders(referer string) map[string]string {
	headers := map[string]string{
		"User-Agent":      sess.pcClientUserAgent(),
		"Accept":          "*/*",
		"Referer":         referer,
		"Origin":          "",
		"Hujiang-App-Key": mddclassPCWebKey,
		"SKsight-App-Key": mddclassPCWebKey,
		"X-CC-COMPANY":    mddclassFirstText(sess.CompanyDomain, mddclassCompanyDomain),
		"Cookie":          sess.Cookie,
		"cookie":          sess.Cookie,
	}
	sess.applyAuthHeaders(headers)
	return headers
}

func (sess *mddclassSession) mediaHeaders(video mddclassVideo) map[string]string {
	headers := sess.webHeaders(mddclassFirstText(video.CompanyDomain, sess.CompanyDomain), mddclassLessonReferer(video.VideoID, video.SeriesID), "*/*")
	headers["User-Agent"] = mddclassUserAgent
	return headers
}

func (sess *mddclassSession) applyAuthHeaders(headers map[string]string) {
	if value := mddclassFirstText(sess.Auth["userSign"], sess.Auth["user_sign"]); value != "" {
		headers["X-User-Sign"] = value
	}
	if value := mddclassFirstText(sess.Auth["keysEncrypt"], sess.Auth["keys_encrypt"]); value != "" {
		headers["X-Keys-Encrypt"] = value
	}
	if value := mddclassFirstText(sess.Auth["feAuth"], sess.Auth["fe_auth"]); value != "" {
		headers["Fe-Auth"] = value
	}
}

func (sess *mddclassSession) pcClientUserAgent() string {
	if ua := mddclassFirstText(sess.Auth["HJUserAgent"], sess.CookieMap["HJUserAgent"], sess.CookieMap["hjuseragent"]); ua != "" {
		return ua
	}
	deviceID := mddclassFirstText(sess.Auth["device_id"], sess.Auth["deviceId"], sess.Auth["clientDeviceId"], sess.Auth["tracertNo"], sess.Auth["TracetNo"])
	if deviceID != "" {
		deviceID = "/" + deviceID
	}
	return fmt.Sprintf(mddclassPCClientAgentTmpl, deviceID)
}

func (sess *mddclassSession) tracertHeaders() map[string]string {
	headers := map[string]string{}
	if value := mddclassFirstText(sess.Auth["tracertNo"], sess.Auth["TracetNo"], sess.Auth["deviceId"]); value != "" {
		headers["TracetNo"] = value
		headers["tracertNo"] = value
	}
	if value := mddclassFirstText(sess.Auth["newTracetNo"], sess.Auth["newTracertNo"]); value != "" {
		headers["NewTracetNo"] = value
		headers["newTracetNo"] = value
	}
	return headers
}

func mddclassLessonReferer(videoID, seriesID string) string {
	if videoID == "" {
		return mddclassLexueHost + "/"
	}
	ref := mddclassLexueHost + "/v/" + videoID
	if seriesID != "" {
		ref += "?sid=" + url.QueryEscape(seriesID)
	}
	return ref
}
