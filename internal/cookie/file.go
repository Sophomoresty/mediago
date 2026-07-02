package cookie

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func ParseNetscapeFile(path string) ([]*http.Cookie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cookies []*http.Cookie
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}

		domain := fields[0]

		expires, _ := strconv.ParseInt(fields[4], 10, 64)
		value := fields[6]
		c := &http.Cookie{
			Domain: domain,
			Path:   fields[2],
			Secure: strings.EqualFold(fields[3], "TRUE"),
			Name:   fields[5],
			Value:  value,
			Quoted: strings.ContainsAny(value, " +/=|,"),
		}
		if expires > 0 {
			c.Expires = time.Unix(expires, 0)
		}
		cookies = append(cookies, c)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("no cookies found in %s", path)
	}
	return cookies, nil
}
