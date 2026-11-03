package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/iterating-io/nats-console/api/internal/auth"
	"github.com/iterating-io/nats-console/api/internal/config"
)

type fakeJWTService struct {
	issueToken string
	issueErr   error
	parseClaim *auth.Claims
	parseErr   error
}

func (f fakeJWTService) Issue(_, _ string) (string, error) {
	return f.issueToken, f.issueErr
}

func (f fakeJWTService) Parse(_ string) (*auth.Claims, error) {
	return f.parseClaim, f.parseErr
}

type fakeNATS struct {
	connected   bool
	requestData map[string]*nats.Msg
	requestErr  map[string]error
	publishes   []string
}

func (f *fakeNATS) IsConnected() bool {
	return f.connected
}

func (f *fakeNATS) Request(subject string, _ []byte, _ time.Duration) (*nats.Msg, error) {
	if err, ok := f.requestErr[subject]; ok {
		return nil, err
	}
	if msg, ok := f.requestData[subject]; ok {
		return msg, nil
	}
	return nil, errors.New("unexpected request: " + subject)
}

func (f *fakeNATS) Publish(subject string, _ []byte) error {
	f.publishes = append(f.publishes, subject)
	return nil
}

func TestWithCORS(t *testing.T) {
	s := &Server{cfg: config.Config{AllowedOrigins: "https://app.example"}}
	hits := 0
	handler := s.withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("unexpected allow origin header: %q", got)
	}
	if hits != 1 {
		t.Fatalf("next handler was not called, hits=%d", hits)
	}

	req = httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status for preflight: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PATCH") {
		t.Fatalf("PATCH missing from preflight methods: %q", got)
	}
}

func TestOriginAllowed(t *testing.T) {
	tests := []struct {
		name, host, origin, configured string
		want                           bool
	}{
		{"same host across proxy TLS termination", "console.example", "https://console.example", "", true},
		{"configured extra origin", "api.example", "https://app.example", "https://app.example, https://other.example", true},
		{"unconfigured origin", "api.example", "https://evil.example", "https://app.example", false},
		{"explicit wildcard", "api.example", "https://evil.example", "*", true},
		{"missing origin", "api.example", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if got := originAllowed(req, tt.configured); got != tt.want {
				t.Fatalf("originAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithCORSDoesNotSetHeadersForSameOriginOrRejectedOrigin(t *testing.T) {
	s := &Server{cfg: config.Config{}}
	for _, origin := range []string{"http://console.example", "https://evil.example"} {
		req := httptest.NewRequest(http.MethodGet, "http://console.example/", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		s.withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("unexpected allow origin for %q: %q", origin, got)
		}
	}
}

func TestHealth(t *testing.T) {
	s := &Server{natsConn: &fakeNATS{connected: true}}
	rec := httptest.NewRecorder()
	s.health(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["status"] != "ok" || got["nats"] != "connected" {
		t.Fatalf("unexpected response body: %#v", got)
	}
}
