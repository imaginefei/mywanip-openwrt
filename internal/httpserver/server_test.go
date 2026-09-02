package httpserver

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mywanip/internal/config"
)

func stubIP(s string) IPFunc {
	return func() (net.IP, error) {
		if s == "" {
			return nil, errors.New("no address")
		}
		return net.ParseIP(s), nil
	}
}

func newTestServer(v4, v6 string) *Server {
	cfg := config.Default()
	cfg.Listen = "127.0.0.1:0"
	return New(cfg, "test", stubIP(v4), stubIP(v6))
}

func TestIPv4Endpoint(t *testing.T) {
	srv := newTestServer("203.0.113.7", "2001:db8::1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ipv4", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if strings.TrimSpace(rec.Body.String()) != "203.0.113.7" {
		t.Errorf("body = %q, want 203.0.113.7", rec.Body.String())
	}
}

func TestIPv4Unavailable(t *testing.T) {
	srv := newTestServer("", "")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ipv4", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestRootJSONAlways200(t *testing.T) {
	srv := newTestServer("", "") // 两个地址都取不到
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["ipv4"] != "" || body["ipv6"] != "" {
		t.Errorf("missing fields should be empty strings, got %v", body)
	}
}

func TestRootJSONValues(t *testing.T) {
	srv := newTestServer("100.64.1.2", "2001:db8::abcd")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["ipv4"] != "100.64.1.2" || body["ipv6"] != "2001:db8::abcd" {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestCORSPreflightAndHeaders(t *testing.T) {
	srv := newTestServer("1.2.3.4", "2001:db8::1")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/ipv4", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("missing CORS Allow-Origin")
	}
	if rec.Header().Get("Server") != "mywanipd/test" {
		t.Errorf("Server header = %q, want mywanipd/test", rec.Header().Get("Server"))
	}

	// GET 响应也应带 CORS 头
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/ipv4", nil))
	if rec2.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("GET response missing CORS header")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer("1.2.3.4", "")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ipv4", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, OPTIONS" {
		t.Errorf("Allow = %q, want 'GET, OPTIONS'", allow)
	}
}

func TestNotFound(t *testing.T) {
	srv := newTestServer("1.2.3.4", "")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/whatever", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
