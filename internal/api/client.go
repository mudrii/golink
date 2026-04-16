package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/mudrii/golink/internal/httprecord"
	"github.com/mudrii/golink/internal/output"
)

const (
	defaultBaseURL        = "https://api.linkedin.com"
	restliProtocolVersion = "2.0.0"
	rateLimitWarnBelow    = 50
)

// ClientConfig controls the construction of the retryable HTTP client.
type ClientConfig struct {
	BaseURL       string
	Token         func(ctx context.Context) (string, error)
	APIVersion    string
	Logger        *slog.Logger
	HTTPClient    *http.Client
	RetryMax      int
	RetryWaitMin  time.Duration
	RetryWaitMax  time.Duration
	UserAgent     string
	RequestIDFunc func() string
}

// Client performs authenticated LinkedIn REST requests with retry, rate-limit
// awareness, and structured error decoding.
type Client struct {
	retryable *retryablehttp.Client
	base      *url.URL
	token     func(ctx context.Context) (string, error)
	apiVers   string
	logger    *slog.Logger
	userAgent string
}

// NewClient constructs a Client using the supplied configuration.
func NewClient(cfg ClientConfig) (*Client, error) {
	baseRaw := cfg.BaseURL
	if baseRaw == "" {
		baseRaw = defaultBaseURL
	}
	base, err := url.Parse(baseRaw)
	if err != nil {
		return nil, fmt.Errorf("parse linkedin base url: %w", err)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}

	// Wrap the base transport with record/replay if env vars are set.
	baseTransport, wrapErr := httprecord.Wrap(
		func() http.RoundTripper {
			if cfg.HTTPClient != nil {
				return cfg.HTTPClient.Transport
			}
			return http.DefaultTransport
		}(),
		os.Getenv("GOLINK_RECORD"),
		os.Getenv("GOLINK_REPLAY"),
	)
	if wrapErr != nil {
		return nil, fmt.Errorf("httprecord: %w", wrapErr)
	}

	client := retryablehttp.NewClient()
	if cfg.HTTPClient != nil {
		client.HTTPClient = cfg.HTTPClient
		if baseTransport != cfg.HTTPClient.Transport {
			// record/replay wrapper applied — use a fresh client with it.
			client.HTTPClient = &http.Client{
				Transport:     baseTransport,
				CheckRedirect: cfg.HTTPClient.CheckRedirect,
				Jar:           cfg.HTTPClient.Jar,
				Timeout:       cfg.HTTPClient.Timeout,
			}
		}
	} else if baseTransport != http.DefaultTransport {
		client.HTTPClient = &http.Client{Transport: baseTransport}
	}

	switch {
	case os.Getenv("GOLINK_RECORD") != "":
		logger.Info("httprecord: recording HTTP exchanges", "path", os.Getenv("GOLINK_RECORD"))
	case os.Getenv("GOLINK_REPLAY") != "":
		logger.Info("httprecord: replaying from cassette", "path", os.Getenv("GOLINK_REPLAY"))
	}
	client.RetryMax = cfg.RetryMax
	if client.RetryMax == 0 {
		client.RetryMax = 3
	}
	client.RetryWaitMin = cfg.RetryWaitMin
	if client.RetryWaitMin == 0 {
		client.RetryWaitMin = 500 * time.Millisecond
	}
	client.RetryWaitMax = cfg.RetryWaitMax
	if client.RetryWaitMax == 0 {
		client.RetryWaitMax = 8 * time.Second
	}
	client.Backoff = retryablehttp.LinearJitterBackoff
	client.Logger = nil
	client.CheckRetry = func(ctx context.Context, resp *http.Response, respErr error) (bool, error) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if respErr != nil {
			return true, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return true, nil
		}
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			return true, nil
		}
		return false, nil
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "golink/0 (+https://github.com/mudrii/golink)"
	}

	return &Client{
		retryable: client,
		base:      base,
		token:     cfg.Token,
		apiVers:   strings.TrimSpace(cfg.APIVersion),
		logger:    logger,
		userAgent: userAgent,
	}, nil
}

// Response bundles parsed payload plus metadata that callers surface in the
// response envelope.
type Response struct {
	Status        int
	Body          []byte
	Header        http.Header
	RequestID     string
	RateLimit     *output.RateLimitInfo
	UnmarshalJSON func(v any) error
}

// Do performs a JSON-bodied request against the LinkedIn REST API. The method
// and path (relative to BaseURL) are required. body is marshaled when non-nil.
// Rest.li requests require X-Restli-Protocol-Version; versioned endpoint
// families also require Linkedin-Version when configured.
func (c *Client) Do(ctx context.Context, method, relativePath string, body any) (*Response, error) {
	u, err := c.resolveURL(relativePath)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Restli-Protocol-Version", restliProtocolVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiVers != "" {
		req.Header.Set("Linkedin-Version", c.apiVers)
	}
	if c.token != nil {
		token, err := c.token(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve access token: %w", err)
		}
		if token == "" {
			return nil, &Error{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED", Message: "no access token available"}
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.retryable.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	requestID := resp.Header.Get("x-restli-id")
	if requestID == "" {
		requestID = resp.Header.Get("X-RestLi-Id")
	}

	rate := parseRateLimit(resp.Header)
	if rate != nil && rate.Remaining != nil && *rate.Remaining >= 0 && *rate.Remaining < rateLimitWarnBelow {
		c.logger.Warn("linkedin rate limit low",
			slog.Int("remaining", *rate.Remaining),
			slog.String("reset_at", rate.ResetAt),
			slog.String("endpoint", relativePath),
		)
	}

	c.logger.Debug("linkedin request",
		slog.String("method", method),
		slog.String("endpoint", u.Path),
		slog.Int("status", resp.StatusCode),
		slog.String("request_id", requestID),
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := decodeError(resp.StatusCode, payload, requestID)
		return nil, apiErr
	}

	return &Response{
		Status:    resp.StatusCode,
		Body:      payload,
		Header:    resp.Header,
		RequestID: requestID,
		RateLimit: rate,
		UnmarshalJSON: func(v any) error {
			if len(payload) == 0 {
				return nil
			}
			return json.Unmarshal(payload, v)
		},
	}, nil
}

func (c *Client) resolveURL(relativePath string) (*url.URL, error) {
	if relativePath == "" {
		return nil, fmt.Errorf("relative path must not be empty")
	}
	ref, err := url.Parse(relativePath)
	if err != nil {
		return nil, fmt.Errorf("parse relative path: %w", err)
	}
	return c.base.ResolveReference(ref), nil
}

func decodeError(status int, body []byte, requestID string) *Error {
	apiErr := &Error{Status: status, RequestID: requestID, Message: strings.TrimSpace(string(body))}
	var envelope struct {
		Status           int    `json:"status"`
		Code             string `json:"code"`
		Message          string `json:"message"`
		ServiceErrorCode string `json:"serviceErrorCode"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.Message != "" {
			apiErr.Message = envelope.Message
		}
		switch {
		case envelope.Code != "":
			apiErr.Code = envelope.Code
		case envelope.ServiceErrorCode != "":
			apiErr.Code = envelope.ServiceErrorCode
		}
		if envelope.Message != "" && apiErr.Details == "" {
			apiErr.Details = envelope.Message
		}
	}
	return apiErr
}

// parseRateLimit extracts LinkedIn rate-limit headers into a RateLimitInfo.
// LinkedIn has historically used a few different header names for rate limit
// data; we accept the most common forms.
func parseRateLimit(header http.Header) *output.RateLimitInfo {
	remaining := firstHeader(header, "X-RateLimit-Remaining", "X-Ratelimit-Remaining", "RateLimit-Remaining")
	reset := firstHeader(header, "X-RateLimit-Reset", "X-Ratelimit-Reset", "RateLimit-Reset")
	if remaining == "" && reset == "" {
		return nil
	}

	info := &output.RateLimitInfo{}
	if remaining != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(remaining)); err == nil {
			info.Remaining = &v
		}
	}
	if reset != "" {
		info.ResetAt = normalizeRateReset(reset)
	}
	if info.Remaining == nil && info.ResetAt == "" {
		return nil
	}
	return info
}

func firstHeader(header http.Header, keys ...string) string {
	for _, k := range keys {
		if v := header.Get(k); v != "" {
			return v
		}
	}
	return ""
}

func normalizeRateReset(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if secs, err := strconv.ParseInt(trimmed, 10, 64); err == nil && secs > 0 {
		return time.Unix(secs, 0).UTC().Format(time.RFC3339)
	}
	if _, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return trimmed
	}
	return trimmed
}
