// Package napcat implements one bounded, read-only OneBot status request.
package napcat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
)

type Result struct {
	Online     bool
	Success    bool
	Category   string
	StatusCode int
	Duration   time.Duration
	Completed  time.Time
}

type Client struct {
	http     *http.Client
	endpoint string
	token    string
}

// Categories are controlled diagnostic identifiers, never upstream error text.
const (
	CategoryRequest      = "request"
	CategoryNetwork      = "network"
	CategoryTimeout      = "timeout"
	CategoryCanceled     = "canceled"
	CategoryAuth         = "auth"
	CategoryHTTP         = "http"
	CategoryBodyTooLarge = "body_too_large"
	CategoryJSON         = "json"
	CategorySchema       = "schema"
	CategoryBusiness     = "business"
	CategoryOK           = "ok"
)

func New(base *url.URL, token string, timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.MaxConnsPerHost = contract.MaxConcurrentScrapes
	transport.MaxIdleConnsPerHost = contract.MaxConcurrentScrapes
	return &Client{http: &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: base.JoinPath(contract.Action).String(), token: token}
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) Collect(ctx context.Context) (result Result) {
	start := time.Now()
	defer func() { result.Completed = time.Now(); result.Duration = time.Since(start) }()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewBufferString("{}"))
	if err != nil {
		result.Category = CategoryRequest
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		result.Category = failureCategory(err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	result.StatusCode = resp.StatusCode
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Category = CategoryAuth
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		result.Category = CategoryHTTP
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, contract.MaxResponseBytes+1))
	if err != nil {
		result.Category = failureCategory(err)
		return
	}
	if len(body) > contract.MaxResponseBytes {
		result.Category = CategoryBodyTooLarge
		return
	}
	if !json.Valid(body) {
		result.Category = CategoryJSON
		return
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		result.Category = CategorySchema
		return
	}
	status, statusOK := field[string](envelope, contract.FieldStatus)
	retcode, retcodeOK := field[int](envelope, contract.FieldRetcode)
	if !statusOK || !retcodeOK {
		result.Category = CategorySchema
		return
	}
	if status != contract.StatusOK || retcode != contract.RetcodeOK {
		result.Category = CategoryBusiness
		return
	}
	data, dataOK := field[map[string]json.RawMessage](envelope, contract.FieldData)
	online, onlineOK := field[bool](data, contract.FieldOnline)
	if !dataOK || !onlineOK {
		result.Category = CategorySchema
		return
	}
	result.Success = true
	result.Online = online
	result.Category = CategoryOK
	return
}

func field[T any](object map[string]json.RawMessage, key string) (value T, ok bool) {
	raw, exists := object[key]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return value, false
	}
	err := json.Unmarshal(raw, &value)
	return value, err == nil
}

func failureCategory(err error) string {
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CategoryTimeout
	}
	if errors.Is(err, context.Canceled) {
		return CategoryCanceled
	}
	return CategoryNetwork
}
