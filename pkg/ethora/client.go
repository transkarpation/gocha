// Package ethora is an HTTP client for the Ethora chat platform API
// (https://ethora.com). Requests are authenticated with a server JWT
// built from the application's API key and secret.
package ethora

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const defaultTimeout = 10 * time.Second

type Client struct {
	baseURL   string
	apiKey    string
	apiSecret string
	httpc     *http.Client
}

func NewClient(baseURL, apiKey, apiSecret string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		apiSecret: apiSecret,
		httpc:     &http.Client{Timeout: defaultTimeout},
	}
}

// AppToken builds the server-to-server JWT that Ethora expects in the
// Authorization header: {"data":{"type":"server","appId":<api key>}}
// signed with the API secret (HS256). The nesting under "data" is required —
// with top-level claims the API rejects the token as INVALID_TOKEN_TYPE.
func (c *Client) AppToken() (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"data": map[string]string{
			"type":  "server",
			"appId": c.apiKey,
		},
	})
	return tok.SignedString([]byte(c.apiSecret))
}

// APIError is returned for non-2xx responses.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ethora api: status %d: %s", e.StatusCode, e.Body)
}

// do sends an authenticated JSON request and decodes the response into out
// (skipped when out is nil).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	params := "<empty>"
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(data)
		params = redactPasswords(data)
	}
	slog.InfoContext(ctx, "ethora request",
		"method", method, "path", path, "params", params)

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	token, err := c.AppToken()
	if err != nil {
		return fmt.Errorf("sign app token: %w", err)
	}
	req.Header.Set("Authorization", token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	slog.InfoContext(ctx, "ethora response",
		"method", method, "path", path, "status", resp.StatusCode, "body", redactPasswords(data))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// redactPasswords renders a JSON request or response body for logging with
// every password-carrying value masked ("password", "xmppPassword", ...):
// mirror passwords are one-off and must never be persisted anywhere,
// logs included.
func redactPasswords(data []byte) string {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return "<non-json body>"
	}
	masked, err := json.Marshal(maskPasswords(v))
	if err != nil {
		return "<unloggable body>"
	}
	return string(masked)
}

func maskPasswords(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if strings.Contains(strings.ToLower(k), "password") {
				t[k] = "[redacted]"
			} else {
				t[k] = maskPasswords(val)
			}
		}
	case []any:
		for i, val := range t {
			t[i] = maskPasswords(val)
		}
	}
	return v
}
