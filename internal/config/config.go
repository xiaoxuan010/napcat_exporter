// Package config parses and validates startup configuration without exposing tokens.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
)

const (
	APIURL      = "napcat.api-url"
	Token       = "napcat.token"
	Timeout     = "napcat.timeout"
	Listen      = "web.listen-address"
	MetricsPath = "web.telemetry-path"
	LogLevel    = "log.level"
)

// Option is the single definition of a CLI option, its environment key and default.
type Option struct{ Name, Env, Default, Help string }

var Options = []Option{
	{APIURL, "NAPCAT_API_URL", "", "OneBot HTTP API base URL (required; path prefixes supported)"},
	{Token, "NAPCAT_TOKEN", "", "OneBot bearer token (optional)"},
	{Timeout, "NAPCAT_TIMEOUT", "3s", "Upstream request timeout; must be below Prometheus scrape_timeout"},
	{Listen, "NAPCAT_WEB_LISTEN_ADDRESS", "127.0.0.1:18599", "Exporter listen address"},
	{MetricsPath, "NAPCAT_WEB_TELEMETRY_PATH", "/metrics", "Metrics HTTP path"},
	{LogLevel, "NAPCAT_LOG_LEVEL", "info", "Log level: debug, info, warn or error"},
}

type Config struct {
	APIURL      *url.URL
	Token       string
	Timeout     time.Duration
	Listen      string
	MetricsPath string
	LogLevel    slog.Level
	Version     bool
}

func Parse(args []string, lookup func(string) (string, bool), output io.Writer) (Config, error) {
	fs := flag.NewFlagSet("napcat_exporter", flag.ContinueOnError)
	// flag's parse errors include supplied values; never forward them to users.
	fs.SetOutput(io.Discard)
	values := make(map[string]*string, len(Options))
	for _, opt := range Options {
		value := opt.Default
		if v, ok := lookup(opt.Env); ok {
			value = v
		}
		values[opt.Name] = fs.String(opt.Name, value, opt.Help+" (env: "+opt.Env+")")
		// Help describes built-in defaults, never environment-injected credentials.
		fs.Lookup(opt.Name).DefValue = opt.Default
	}
	help := fs.Bool("help", false, "Show help")
	version := fs.Bool("version", false, "Show build version")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(output)
			fs.PrintDefaults()
			return Config{}, flag.ErrHelp
		}
		return Config{}, errors.New("invalid command line; use --help for supported flags")
	}
	if *help {
		fs.SetOutput(output)
		fs.PrintDefaults()
		return Config{}, flag.ErrHelp
	}
	if fs.NArg() != 0 {
		return Config{}, errors.New("unexpected positional arguments; use --help")
	}
	if *version {
		return Config{Version: true}, nil
	}
	get := func(key string) string { return *values[key] }
	u, err := url.Parse(get(APIURL))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return Config{}, invalid(APIURL, "provide an http(s) base URL without credentials, query or fragment")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Config{}, invalid(APIURL, "provide a valid port")
		}
	}
	timeout, err := time.ParseDuration(get(Timeout))
	if err != nil || timeout <= 0 {
		return Config{}, invalid(Timeout, "provide a positive duration")
	}
	if strings.ContainsAny(get(Token), "\r\n") {
		return Config{}, invalid(Token, "remove newline characters")
	}
	_, port, err := net.SplitHostPort(get(Listen))
	if err != nil {
		return Config{}, invalid(Listen, "provide host:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return Config{}, invalid(Listen, "provide a valid numeric port")
	}
	mp := get(MetricsPath)
	pathURL, err := url.ParseRequestURI(mp)
	if err != nil || !strings.HasPrefix(mp, "/") || path.Clean(mp) != mp || strings.ContainsAny(mp, "{}# \t\r\n") || pathURL == nil || pathURL.RawQuery != "" || pathURL.ForceQuery || pathURL.Fragment != "" || pathURL.Path != mp || mp == "/" || mp == contract.HealthPath {
		return Config{}, invalid(MetricsPath, "provide an absolute path distinct from / and the health endpoint, without escaping or query")
	}
	var level slog.Level
	switch strings.ToLower(get(LogLevel)) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return Config{}, invalid(LogLevel, "use debug, info, warn or error")
	}
	return Config{APIURL: u, Token: get(Token), Timeout: timeout, Listen: get(Listen), MetricsPath: mp, LogLevel: level}, nil
}

func invalid(field, hint string) error { return fmt.Errorf("invalid %s: %s", field, hint) }
