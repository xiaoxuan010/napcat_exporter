// Package acceptance runs opt-in tests against real processes and installed packages.
package acceptance_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/xiaoxuan010/napcat_exporter/internal/config"
	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
)

func envName(name string) string {
	for _, option := range config.Options {
		if option.Name == name {
			return option.Env
		}
	}
	panic("unknown option")
}
func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func waitFor(t *testing.T, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + description)
}

func startProcess(t *testing.T, binary string, args []string, env []string) *exec.Cmd {
	t.Helper()
	log, err := os.CreateTemp(t.TempDir(), "process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Log(string(data))
		}
	})
	return cmd
}

func TestPrometheus(t *testing.T) {
	exporter := os.Getenv("NAPCAT_E2E_BINARY")
	prometheus := os.Getenv("PROMETHEUS_BINARY")
	promtool := os.Getenv("PROMTOOL_BINARY")
	if exporter == "" || prometheus == "" || promtool == "" {
		t.Skip("set NAPCAT_E2E_BINARY, PROMETHEUS_BINARY and PROMTOOL_BINARY to run process acceptance")
	}
	var mode atomic.Int32
	var requests atomic.Int64
	token := rand.Text()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if mode.Load() < 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{contract.FieldStatus: contract.StatusOK, contract.FieldRetcode: contract.RetcodeOK, contract.FieldData: map[string]any{contract.FieldOnline: mode.Load() == 0}})
	}))
	defer upstream.Close()
	address := freeAddress(t)
	metricsPath := "/" + rand.Text()
	exporterProcess := startProcess(t, exporter, nil, []string{envName(config.APIURL) + "=" + upstream.URL, envName(config.Token) + "=" + token, envName(config.Listen) + "=" + address, envName(config.MetricsPath) + "=" + metricsPath})
	client := &http.Client{Timeout: time.Second}
	waitFor(t, "exporter health", func() bool {
		resp, err := client.Get("http://" + address + contract.HealthPath)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	if requests.Load() != 0 {
		t.Fatal("exporter collected without a metrics request")
	}
	resp, err := client.Get("http://" + address + metricsPath)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	check := exec.Command(promtool, "check", "metrics")
	check.Stdin = bytes.NewReader(body)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("promtool rejected metrics: %v\n%s", err, output)
	}
	if output, err := exec.Command(promtool, "check", "rules", filepath.Join("..", "..", "examples", "alerts.yml")).CombinedOutput(); err != nil {
		t.Fatalf("promtool rejected alert rules: %v\n%s", err, output)
	}
	promAddress := freeAddress(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "prometheus.yml")
	text := fmt.Sprintf("global:\n  scrape_interval: 200ms\n  scrape_timeout: 150ms\nscrape_configs:\n  - job_name: acceptance\n    metrics_path: %q\n    static_configs:\n      - targets: [%q]\n", metricsPath, address)
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	startProcess(t, prometheus, []string{"--config.file=" + file, "--storage.tsdb.path=" + filepath.Join(dir, "data"), "--web.listen-address=" + promAddress}, nil)
	query := func(expression string) ([]float64, bool) {
		resp, err := client.Get("http://" + promAddress + "/api/v1/query?query=" + url.QueryEscape(expression))
		if err != nil {
			return nil, false
		}
		defer func() { _ = resp.Body.Close() }()
		var result struct {
			Status string `json:"status"`
			Data   struct {
				Result []struct {
					Value []json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if json.NewDecoder(resp.Body).Decode(&result) != nil || result.Status != "success" {
			return nil, false
		}
		var values []float64
		for _, sample := range result.Data.Result {
			if len(sample.Value) != 2 {
				return nil, false
			}
			var text string
			if json.Unmarshal(sample.Value[1], &text) != nil {
				return nil, false
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, false
			}
			values = append(values, value)
		}
		return values, true
	}
	is := func(expression string, want float64) bool {
		values, ok := query(expression)
		return ok && len(values) == 1 && values[0] == want
	}
	waitFor(t, "online metrics in Prometheus", func() bool {
		return is("up", contract.Up) && is(contract.APIUp, contract.Up) && is(contract.Online, contract.Up)
	})
	mode.Store(1)
	waitFor(t, "offline metrics in Prometheus", func() bool { return is(contract.APIUp, contract.Up) && is(contract.Online, contract.Down) })
	mode.Store(-1)
	waitFor(t, "unknown state and stale online removal", func() bool {
		values, ok := query(contract.Online)
		return ok && len(values) == 0 && is("up", contract.Up) && is(contract.APIUp, contract.Down)
	})
	mode.Store(0)
	waitFor(t, "upstream recovery", func() bool { return is(contract.APIUp, contract.Up) && is(contract.Online, contract.Up) })
	if strings.Contains(string(body), token) {
		t.Fatal("metrics exposed token")
	}
	if err := exporterProcess.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- exporterProcess.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatal("exporter did not shut down cleanly", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exporter did not shut down within grace period")
	}
	waitFor(t, "Prometheus detecting exporter shutdown", func() bool { return is("up", contract.Down) })
}

func command(t *testing.T, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
	}
	return strings.TrimSpace(string(output))
}
