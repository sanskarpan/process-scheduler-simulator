package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func nopHandler(w http.ResponseWriter, r *http.Request) {}

// ISSUE-031: CORS must not set ACAO for unrecognised or absent origins.

func TestCORSAllowedOriginEchoedExactly(t *testing.T) {
	h := CORS([]string{"https://trusted.example.com"})(http.HandlerFunc(nopHandler))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://trusted.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got != "https://trusted.example.com" {
		t.Errorf("ACAO = %q, want exact origin", got)
	}
}

func TestCORSRejectedOriginNoHeader(t *testing.T) {
	h := CORS([]string{"https://trusted.example.com"})(http.HandlerFunc(nopHandler))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://attacker.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got != "" {
		t.Errorf("ACAO = %q for rejected origin, want empty string", got)
	}
}

func TestCORSNoOriginHeaderSetsNoACAO(t *testing.T) {
	h := CORS([]string{"https://trusted.example.com"})(http.HandlerFunc(nopHandler))
	req := httptest.NewRequest("GET", "/", nil)
	// No Origin header at all
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got != "" {
		t.Errorf("ACAO = %q for no-origin request, want empty string", got)
	}
}

func TestCORSMultipleAllowedOrigins(t *testing.T) {
	h := CORS([]string{"https://a.example.com", "https://b.example.com"})(http.HandlerFunc(nopHandler))

	for _, origin := range []string{"https://a.example.com", "https://b.example.com"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Header().Get("Access-Control-Allow-Origin")
		if got != origin {
			t.Errorf("ACAO = %q for origin %q, want exact origin", got, origin)
		}
	}
}

func TestCORSWildcardConfig(t *testing.T) {
	h := CORS([]string{"*"})(http.HandlerFunc(nopHandler))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://anyone.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Access-Control-Allow-Origin")
	// originAllowed returns true for "*", so ACAO should be echoed as the request origin
	if got != "https://anyone.com" {
		t.Errorf("ACAO = %q for wildcard config, want request origin echoed", got)
	}
}

func TestCORSNoListNoHeader(t *testing.T) {
	// Empty allowlist: no origin should ever receive an ACAO header
	h := CORS([]string{})(http.HandlerFunc(nopHandler))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://anything.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got != "" {
		t.Errorf("ACAO = %q for empty allowlist, want empty string", got)
	}
}
