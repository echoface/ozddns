package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient is the shared client used by every provider adapter.
var HTTPClient = &http.Client{Timeout: 15 * time.Second}

// DoJSON performs an HTTP request with an optional JSON body and decodes a JSON
// response into out. A non-2xx status is returned as an error carrying the body.
func DoJSON(ctx context.Context, method, url string, headers map[string]string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, redactURL(url), resp.StatusCode, Truncate(string(data), 300))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decoding %s response: %w", redactURL(url), err)
		}
	}
	return nil
}

func redactURL(u string) string { return u }

// DoJSONBody executes a prebuilt request and decodes a JSON response into out.
func DoJSONBody(ctx context.Context, req *http.Request, out any) error {
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", req.Method, redactURL(req.URL.String()), resp.StatusCode, Truncate(string(data), 300))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decoding %s response: %w", redactURL(req.URL.String()), err)
		}
	}
	return nil
}

// Truncate clips s to n bytes for error messages.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
