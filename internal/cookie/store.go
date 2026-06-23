package cookie

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"golang.org/x/net/publicsuffix"
)

type Store struct {
	jar http.CookieJar
}

func NewStore() *Store {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &Store{jar: jar}
}

func (s *Store) Jar() http.CookieJar {
	return s.jar
}

func (s *Store) LoadFromFile(path string) error {
	cookies, err := ParseNetscapeFile(path)
	if err != nil {
		return err
	}
	grouped := make(map[string][]*http.Cookie)
	for _, c := range cookies {
		grouped[c.Domain] = append(grouped[c.Domain], c)
	}
	for domain, cs := range grouped {
		scheme := "https"
		u := &url.URL{Scheme: scheme, Host: domain, Path: "/"}
		s.jar.SetCookies(u, cs)
	}
	return nil
}

func (s *Store) LoadFromBrowser(browser string) error {
	cookies, err := ReadBrowserCookies(browser)
	if err != nil {
		return err
	}
	grouped := make(map[string][]*http.Cookie)
	for _, c := range cookies {
		grouped[c.Domain] = append(grouped[c.Domain], c)
	}
	for domain, cs := range grouped {
		u := &url.URL{Scheme: "https", Host: domain, Path: "/"}
		s.jar.SetCookies(u, cs)
	}
	return nil
}
