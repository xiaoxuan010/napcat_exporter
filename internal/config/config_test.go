package config_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/xiaoxuan010/napcat_exporter/internal/config"
)

func envKey(name string) string {
	for _, opt := range config.Options {
		if opt.Name == name {
			return opt.Env
		}
	}
	panic("unknown test option")
}
func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
}

func TestExplicitFlagsOverrideEnvironmentBeforeValidation(t *testing.T) {
	token := rand.Text()
	env := map[string]string{envKey(config.APIURL): "not-a-url", envKey(config.Timeout): "invalid-duration", envKey(config.Token): rand.Text(), envKey(config.MetricsPath): "invalid-path"}
	path := "/" + rand.Text()
	duration := 137 * time.Millisecond
	cfg, err := config.Parse([]string{"--" + config.APIURL, "http://localhost:32123/prefix", "--" + config.Timeout, duration.String(), "--" + config.Token, token, "--" + config.MetricsPath, path}, lookup(env), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != token || cfg.Timeout != duration || cfg.MetricsPath != path || cfg.APIURL.Path != "/prefix" {
		t.Fatal("explicit inputs did not take effect")
	}
	// Explicitly clearing the token must override a configured environment value.
	cfg, err = config.Parse([]string{"--" + config.APIURL, "http://localhost", "--" + config.Timeout, duration.String(), "--" + config.MetricsPath, path, "--" + config.Token, ""}, lookup(env), &bytes.Buffer{})
	if err != nil || cfg.Token != "" {
		t.Fatal("explicit empty flag did not clear environment token")
	}
}

func TestEnvironmentIsUsedAndDiagnosticsNeverEchoInputs(t *testing.T) {
	token := rand.Text()
	env := map[string]string{envKey(config.APIURL): "https://localhost", envKey(config.Token): token}
	cfg, err := config.Parse(nil, lookup(env), &bytes.Buffer{})
	if err != nil || cfg.Token != token {
		t.Fatal("environment input not used", err)
	}
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		var output bytes.Buffer
		_, err := config.Parse(args, lookup(env), &output)
		if !errors.Is(err, flag.ErrHelp) || strings.Contains(output.String(), token) {
			t.Fatal("help failed or exposed token")
		}
	}
	for _, args := range [][]string{{"--unknown=" + token}, {"--" + config.Timeout, token}, {"--" + config.APIURL, "http://" + token + "@localhost"}, {token}} {
		var output bytes.Buffer
		_, err := config.Parse(args, lookup(env), &output)
		if err == nil || strings.Contains(err.Error()+output.String(), token) {
			t.Fatal("invalid input accepted or echoed")
		}
	}
}

func TestRejectInvalidEffectiveConfiguration(t *testing.T) {
	cases := map[string][]string{
		config.APIURL:      {"", "ftp://localhost", "http://localhost?token=x", "http://localhost#fragment", "http://localhost:99999", "http://localhost?"},
		config.Timeout:     {"0s", "-1s", "nonsense"},
		config.Listen:      {"localhost", "localhost:-2", "localhost:70000"},
		config.MetricsPath: {"/", "relative", "/foo?bar", "/foo#bar", "/foo/{wildcard}", "/foo%2Fbar", "/foo?"},
		config.LogLevel:    {"verbose"},
		config.Token:       {"abc\nxyz", "abc\rxyz"},
	}
	for field, values := range cases {
		for _, value := range values {
			t.Run(field+"/"+value, func(t *testing.T) {
				env := map[string]string{envKey(config.APIURL): "http://localhost", envKey(field): value}
				_, err := config.Parse(nil, lookup(env), &bytes.Buffer{})
				if err == nil || !strings.Contains(err.Error(), field) {
					t.Fatal("missing field-specific validation", err)
				}
			})
		}
	}
}
