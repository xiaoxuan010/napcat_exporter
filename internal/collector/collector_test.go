package collector_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/xiaoxuan010/napcat_exporter/internal/collector"
	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
	"github.com/xiaoxuan010/napcat_exporter/internal/napcat"
	"github.com/xiaoxuan010/napcat_exporter/internal/server"
)

func success(online bool) map[string]any {
	return map[string]any{contract.FieldStatus: contract.StatusOK, contract.FieldRetcode: contract.RetcodeOK, contract.FieldData: map[string]any{contract.FieldOnline: online}}
}
func encoded(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type upstream struct {
	mu            sync.Mutex
	body          string
	code          int
	calls         atomic.Int64
	handler       *httptest.Server
	token         string
	prefix        string
	requestErrors chan string
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{token: rand.Text(), prefix: "/" + rand.Text(), requestErrors: make(chan string, 64)}
	u.set(encoded(success(true)), http.StatusOK)
	u.handler = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		body, err := io.ReadAll(r.Body)
		var request map[string]json.RawMessage
		if err != nil || json.Unmarshal(body, &request) != nil || request == nil || len(request) != 0 || r.Method != http.MethodPost || r.URL.Path != u.prefix+"/"+contract.Action || r.Header.Get("Authorization") != "Bearer "+u.token {
			u.requestErrors <- "invalid upstream request"
		}
		u.mu.Lock()
		payload, code := u.body, u.code
		u.mu.Unlock()
		w.WriteHeader(code)
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(func() {
		u.handler.Close()
		close(u.requestErrors)
		for err := range u.requestErrors {
			t.Error(err)
		}
	})
	return u
}
func (u *upstream) set(body string, code int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.body = body
	u.code = code
}

func endpoint(t *testing.T, u *upstream) (string, *httptest.Server) {
	t.Helper()
	base, err := url.Parse(u.handler.URL + u.prefix + "/")
	if err != nil {
		t.Fatal(err)
	}
	client := napcat.New(base, u.token, time.Second)
	t.Cleanup(client.Close)
	metricsPath := "/" + rand.Text()
	h := collector.New(client, slog.New(slog.NewJSONHandler(io.Discard, nil)), collector.Build{Version: rand.Text(), Revision: rand.Text()})
	srv := httptest.NewServer(server.New(metricsPath, h))
	t.Cleanup(srv.Close)
	return srv.URL + metricsPath, srv
}

func scrape(t *testing.T, endpoint string) map[string]*dto.MetricFamily {
	t.Helper()
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scrape returned %s", resp.Status)
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return families
}
func gauge(t *testing.T, families map[string]*dto.MetricFamily, name string) float64 {
	t.Helper()
	family := families[name]
	if family == nil || family.GetType() != dto.MetricType_GAUGE || len(family.Metric) != 1 {
		t.Fatalf("missing or invalid gauge %s", name)
	}
	return family.Metric[0].GetGauge().GetValue()
}
func missing(t *testing.T, families map[string]*dto.MetricFamily, name string) {
	t.Helper()
	if families[name] != nil {
		t.Fatalf("unexpected sample %s", name)
	}
}

func TestStateLifecycleOverHTTP(t *testing.T) {
	u := newUpstream(t)
	ep, srv := endpoint(t, u)
	// Health must neither call nor depend on the upstream.
	resp, err := http.Get(srv.URL + contract.HealthPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || u.calls.Load() != 0 {
		t.Fatal("health performed collection")
	}
	u.set(encoded(success(true)), http.StatusForbidden)
	first := scrape(t, ep)
	if gauge(t, first, contract.APIUp) != contract.Down {
		t.Fatal("auth failure reported up")
	}
	missing(t, first, contract.Online)
	missing(t, first, contract.LastSuccess)
	u.set(encoded(success(true)), http.StatusOK)
	before := float64(time.Now().UnixNano()) / float64(time.Second)
	online := scrape(t, ep)
	stamp := gauge(t, online, contract.LastSuccess)
	if gauge(t, online, contract.APIUp) != contract.Up || gauge(t, online, contract.Online) != contract.Up || stamp < before || stamp > float64(time.Now().UnixNano())/float64(time.Second) {
		t.Fatal("online response or timestamp invalid")
	}
	u.set("{", http.StatusOK)
	failed := scrape(t, ep)
	missing(t, failed, contract.Online)
	if gauge(t, failed, contract.LastSuccess) != stamp || gauge(t, failed, contract.APIUp) != contract.Down {
		t.Fatal("failure lost previous success or returned stale online")
	}
	u.set(encoded(success(false)), http.StatusOK)
	offline := scrape(t, ep)
	if gauge(t, offline, contract.APIUp) != contract.Up || gauge(t, offline, contract.Online) != contract.Down || gauge(t, offline, contract.LastSuccess) <= stamp {
		t.Fatal("offline response did not recover and update success time")
	}
	for _, samples := range []map[string]*dto.MetricFamily{first, online, failed, offline} {
		if gauge(t, samples, contract.Duration) < 0 {
			t.Fatal("negative duration")
		}
	}
	if u.calls.Load() != 4 {
		t.Fatal("a scrape did not perform exactly one upstream request")
	}
}

func TestInvalidResponseMatrixOverHTTP(t *testing.T) {
	u := newUpstream(t)
	ep, _ := endpoint(t, u)
	cases := map[string]string{"invalid_json": "{", "root_null": "null", "root_array": "[]", "trailing_json": encoded(success(true)) + "{}"}
	for _, field := range []string{contract.FieldStatus, contract.FieldRetcode, contract.FieldData} {
		obj := success(true)
		delete(obj, field)
		cases["missing_"+field] = encoded(obj)
		for i, value := range []any{nil, []any{}, false, rand.Text()} {
			obj := success(true)
			obj[field] = value
			cases[fmt.Sprintf("bad_%s_%d", field, i)] = encoded(obj)
		}
	}
	for i, value := range []any{nil, rand.Text(), 42, []any{}, map[string]any{}} {
		obj := success(true)
		obj[contract.FieldData] = map[string]any{contract.FieldOnline: value}
		cases[fmt.Sprintf("bad_online_%d", i)] = encoded(obj)
	}
	obj := success(true)
	obj[contract.FieldData] = map[string]any{}
	cases["missing_online"] = encoded(obj)
	obj = success(true)
	obj[contract.FieldStatus] = rand.Text()
	cases["business_status"] = encoded(obj)
	obj = success(true)
	obj[contract.FieldRetcode] = contract.RetcodeOK + 1
	cases["business_retcode"] = encoded(obj)
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			before := u.calls.Load()
			u.set(body, http.StatusOK)
			samples := scrape(t, ep)
			if gauge(t, samples, contract.APIUp) != contract.Down {
				t.Fatal("invalid response accepted")
			}
			missing(t, samples, contract.Online)
			missing(t, samples, contract.LastSuccess)
			gauge(t, samples, contract.Duration)
			if u.calls.Load() != before+1 {
				t.Fatal("unexpected retry")
			}
		})
	}
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError, http.StatusBadGateway, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			u.set(encoded(success(true)), code)
			samples := scrape(t, ep)
			if gauge(t, samples, contract.APIUp) != contract.Down {
				t.Fatal("HTTP failure accepted")
			}
			missing(t, samples, contract.Online)
		})
	}
	obj = success(false)
	obj["future_extension"] = rand.Text()
	u.set(encoded(obj), http.StatusOK)
	samples := scrape(t, ep)
	if gauge(t, samples, contract.APIUp) != contract.Up {
		t.Fatal("unknown extension rejected")
	}
}

func TestTimeoutAndConcurrencyDoNotBlockHealth(t *testing.T) {
	started := make(chan struct{}, contract.MaxConcurrentScrapes)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = io.WriteString(w, encoded(success(true)))
	}))
	defer upstream.Close()
	defer close(release)
	base, _ := url.Parse(upstream.URL)
	client := napcat.New(base, "", 200*time.Millisecond)
	defer client.Close()
	h := collector.New(client, slog.New(slog.NewJSONHandler(io.Discard, nil)), collector.Build{})
	path := "/" + rand.Text()
	srv := httptest.NewServer(server.New(path, h))
	defer srv.Close()
	results := make(chan *http.Response, contract.MaxConcurrentScrapes)
	requestErrors := make(chan error, contract.MaxConcurrentScrapes)
	for range contract.MaxConcurrentScrapes {
		go func() {
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				requestErrors <- err
				return
			}
			results <- resp
		}()
	}
	for range contract.MaxConcurrentScrapes {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("request never reached upstream")
		}
	}
	for _, p := range []string{path, contract.HealthPath} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		want := http.StatusOK
		if p == path {
			want = http.StatusServiceUnavailable
		}
		if resp.StatusCode != want {
			t.Fatalf("unexpected status for %s: %s", p, resp.Status)
		}
	}
	for range contract.MaxConcurrentScrapes {
		select {
		case err := <-requestErrors:
			t.Fatal(err)
		case resp := <-results:
			parser := expfmt.NewTextParser(model.LegacyValidation)
			samples, err := parser.TextToMetricFamilies(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || gauge(t, samples, contract.APIUp) != contract.Down {
				t.Fatal("timeout did not produce successful metrics response with API down")
			}
			missing(t, samples, contract.Online)
		case <-time.After(5 * time.Second):
			t.Fatal("upstream timeout not enforced")
		}
	}
}

func TestTokenNeverAppearsInLogsOrMetrics(t *testing.T) {
	u := newUpstream(t)
	u.set(u.token, http.StatusInternalServerError)
	base, _ := url.Parse(u.handler.URL + u.prefix)
	client := napcat.New(base, u.token, time.Second)
	defer client.Close()
	var logs bytes.Buffer
	h := collector.New(client, slog.New(slog.NewJSONHandler(&logs, nil)), collector.Build{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(logs.String()+rec.Body.String(), u.token) {
		t.Fatal("token leaked")
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal("log is not structured JSON", err)
	}
	if record["category"] != napcat.CategoryHTTP {
		t.Fatal("missing controlled failure category")
	}
}
