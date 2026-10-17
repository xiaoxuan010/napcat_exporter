package napcat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
	"github.com/xiaoxuan010/napcat_exporter/internal/napcat"
)

func TestRedirectNeverFollowsOrForwardsToken(t *testing.T) {
	var redirected atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	base, _ := url.Parse(source.URL)
	client := napcat.New(base, "test-only", time.Second)
	defer client.Close()
	result := client.Collect(context.Background())
	if result.Success || redirected.Load() != 0 {
		t.Fatal("redirect followed or accepted")
	}
}

func TestConnectionFailureAndCanceledRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base, _ := url.Parse(srv.URL)
	srv.Close()
	client := napcat.New(base, "", time.Second)
	defer client.Close()
	result := client.Collect(context.Background())
	if result.Success || result.Category != napcat.CategoryNetwork || result.Completed.IsZero() {
		t.Fatal("connection failure not recorded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = client.Collect(ctx)
	if result.Success || result.Category != napcat.CategoryCanceled {
		t.Fatal("cancelation not honored")
	}
}

func TestOversizedBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat(" ", contract.MaxResponseBytes+1))
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	client := napcat.New(base, "", time.Second)
	defer client.Close()
	result := client.Collect(context.Background())
	if result.Success || result.Category != napcat.CategoryBodyTooLarge {
		t.Fatal("response limit not enforced")
	}
}

func TestAbsentTokenDoesNotSendAuthorization(t *testing.T) {
	header := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	client := napcat.New(base, "", time.Second)
	defer client.Close()
	client.Collect(context.Background())
	if <-header != "" {
		t.Fatal("empty token still produced authentication header")
	}
}
