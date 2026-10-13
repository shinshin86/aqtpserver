package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const modelID = "aquestalkplayer"

type serverConfig struct {
	host           string
	port           int
	executable     string
	defaultPreset  string
	timeout        time.Duration
	maxInputChars  int
	allowedOrigins []string
}

func defaultConfig(platform string) serverConfig {
	cfg := serverConfig{host: "127.0.0.1", port: 20203, timeout: 30 * time.Second, maxInputChars: 1000}
	if platform == "darwin" {
		cfg.executable = "/Applications/AquesTalkPlayer.app/Contents/MacOS/AquesTalkPlayer"
	}
	return cfg
}

func (c *serverConfig) validate(platform string) error {
	if platform != "darwin" && platform != "windows" {
		return fmt.Errorf("unsupported OS %q: AquesTalkPlayer requires macOS or Windows", platform)
	}
	if !isLoopbackHost(c.host) {
		return errors.New("--host must be a loopback address (127.0.0.1, localhost, or ::1)")
	}
	if c.port < 1 || c.port > 65535 {
		return errors.New("--port must be between 1 and 65535")
	}
	if c.timeout <= 0 {
		return errors.New("--timeout must be positive (for example, 30s)")
	}
	if c.maxInputChars <= 0 {
		return errors.New("--max-input-chars must be positive")
	}
	if strings.ContainsRune(c.defaultPreset, 0) {
		return errors.New("--default-preset must not contain NUL characters")
	}
	if c.executable == "" {
		return errors.New("install AquesTalkPlayer yourself, then set --aquestalkplayer-path to AquesTalkPlayer.exe")
	}
	abs, err := filepath.Abs(c.executable)
	if err != nil {
		return errors.New("cannot resolve --aquestalkplayer-path")
	}
	c.executable = abs
	if !executableExists(c.executable) {
		return errors.New("AquesTalkPlayer executable not found: install it yourself and set --aquestalkplayer-path to its executable file")
	}
	for _, origin := range c.allowedOrigins {
		if !validOrigin(origin) {
			return fmt.Errorf("invalid --allowed-origin %q: use an exact http://, https://, or app:// origin without a path or wildcard", origin)
		}
	}
	return nil
}

func executableExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Resolve localhost to a literal address so DNS cannot change the bind target.
func (c serverConfig) listenAddress() string {
	host := c.host
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(c.port))
}

func validRequestHost(authority string) bool {
	host := authority
	if strings.HasPrefix(authority, "[") && strings.HasSuffix(authority, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]")
	} else if strings.Contains(authority, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(authority)
		if err != nil || port == "" {
			return false
		}
		for _, ch := range port {
			if ch < '0' || ch > '9' {
				return false
			}
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return isLoopbackHost(host)
}

func validOrigin(origin string) bool {
	if origin == "app://" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "app") &&
		u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		!strings.ContainsAny(origin, "* \t\r\n#")
}

func (c serverConfig) allowsOrigin(origin string) bool {
	if !validOrigin(origin) {
		return false
	}
	u, _ := url.Parse(origin)
	if u.Scheme == "app" || (u.Scheme == "http" && validRequestHost(u.Host)) {
		return true
	}
	for _, allowed := range c.allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
