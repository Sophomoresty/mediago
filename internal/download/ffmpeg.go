package download

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/Sophomoresty/mediago/internal/util"
)

func runFFmpeg(cmd *exec.Cmd) error {
	out, err := cmd.Output()
	if err == nil {
		_ = out
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		fmt.Fprint(os.Stderr, string(ee.Stderr))
	}
	return err
}

func ffmpegHTTPProxyURL() string {
	proxy := util.DefaultProxy()
	if proxy == "" {
		return ""
	}
	parsed, err := util.ParseProxyURL(proxy)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func ffmpegEnv() []string {
	proxy := util.DefaultProxy()
	if proxy == "" {
		return nil
	}
	return []string{
		"HTTP_PROXY=" + proxy,
		"HTTPS_PROXY=" + proxy,
		"ALL_PROXY=" + proxy,
		"http_proxy=" + proxy,
		"https_proxy=" + proxy,
		"all_proxy=" + proxy,
	}
}
