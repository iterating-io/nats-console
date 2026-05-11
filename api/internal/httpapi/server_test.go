package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	s := &Server{cfg: config.Config{AllowedOrigins: "*"}}
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
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status for preflight: %d", rec.Code)
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
