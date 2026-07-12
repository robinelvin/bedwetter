package alerts

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/robinelvin/bedwetter/config"
)

func TestNtfyPriority(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"alarm", "5"},
		{"warn", "4"},
		{"info", "3"},
		{"unknown", "3"},
		{"", "3"},
	}
	for _, tt := range tests {
		got := ntfyPriority(tt.level)
		if got != tt.want {
			t.Errorf("ntfyPriority(%q): got %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestNtfyTags(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"alarm", "rotating_light,alarm"},
		{"warn", "warning"},
		{"info", "droplet,information_source"},
		{"unknown", "droplet,information_source"},
		{"", "droplet,information_source"},
	}
	for _, tt := range tests {
		got := ntfyTags(tt.level)
		if got != tt.want {
			t.Errorf("ntfyTags(%q): got %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestGenerateNtfyUUID(t *testing.T) {
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	uuid1 := GenerateNtfyUUID()
	if uuid1 == "" {
		t.Fatal("GenerateNtfyUUID() returned empty string")
	}
	if !uuidRe.MatchString(uuid1) {
		t.Errorf("GenerateNtfyUUID() = %q, does not match UUID v4 format", uuid1)
	}

	uuid2 := GenerateNtfyUUID()
	if uuid1 == uuid2 {
		t.Errorf("GenerateNtfyUUID() returned same UUID twice: %q", uuid1)
	}
}

type capturedRequest struct {
	Method string
	URL    string
	Body   string
	Header http.Header
}

func newTestNtfyClient(t *testing.T, ntfyCfg config.NtfyConfig, handler http.HandlerFunc) (*NtfyClient, *httptest.Server) {
	t.Helper()
	cfg := &config.Config{Ntfy: ntfyCfg}
	server := httptest.NewServer(handler)
	cfg.Ntfy.Server = server.URL
	client := NewNtfyClient(cfg)
	return client, server
}

func TestNtfyClientSendDisabled(t *testing.T) {
	cfg := &config.Config{Ntfy: config.NtfyConfig{
		Enabled:   false,
		Server:    "http://localhost:1",
		UUID:      "test-uuid-1234",
		AlertInfo: true,
	}}
	client := NewNtfyClient(cfg)

	client.Send("info", "Test", "hello")
}

func TestNtfyClientSendEmptyServer(t *testing.T) {
	cfg := &config.Config{Ntfy: config.NtfyConfig{
		Enabled:   true,
		Server:    "",
		UUID:      "test-uuid-1234",
		AlertInfo: true,
	}}
	client := NewNtfyClient(cfg)

	client.Send("info", "Test", "hello")
}

func TestNtfyClientSendEmptyUUID(t *testing.T) {
	cfg := &config.Config{Ntfy: config.NtfyConfig{
		Enabled:   true,
		UUID:      "",
		AlertInfo: true,
	}}
	client := NewNtfyClient(cfg)

	client.Send("info", "Test", "hello")
}

func TestNtfyClientSendLevelDisabled(t *testing.T) {
	tests := []struct {
		level    string
		alertCfg config.NtfyConfig
	}{
		{"info", config.NtfyConfig{Enabled: true, UUID: "u", AlertInfo: false, AlertWarn: true, AlertAlarm: true}},
		{"warn", config.NtfyConfig{Enabled: true, UUID: "u", AlertInfo: true, AlertWarn: false, AlertAlarm: true}},
		{"alarm", config.NtfyConfig{Enabled: true, UUID: "u", AlertInfo: true, AlertWarn: true, AlertAlarm: false}},
	}

	for _, tt := range tests {
		cfg := &config.Config{Ntfy: tt.alertCfg}
		client := NewNtfyClient(cfg)
		client.Send(tt.level, "Test", "hello")
	}
}

func TestNtfyClientSendUnknownLevel(t *testing.T) {
	cfg := &config.Config{Ntfy: config.NtfyConfig{
		Enabled:   true,
		UUID:      "test-uuid-1234",
		AlertInfo: true,
		AlertWarn: true,
		AlertAlarm: true,
	}}
	client := NewNtfyClient(cfg)

	client.Send("critical", "Test", "hello")
}

func TestNtfyClientSendSuccess(t *testing.T) {
	tests := []struct {
		level        string
		wantPriority string
		wantTags     string
	}{
		{"info", "3", "droplet,information_source"},
		{"warn", "4", "warning"},
		{"alarm", "5", "rotating_light,alarm"},
	}

	for _, tt := range tests {
		var mu sync.Mutex
		var captured *capturedRequest

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			body, _ := io.ReadAll(r.Body)
			captured = &capturedRequest{
				Method: r.Method,
				URL:    r.URL.String(),
				Body:   string(body),
				Header: r.Header.Clone(),
			}
			w.WriteHeader(http.StatusOK)
		})

		client, server := newTestNtfyClient(t, config.NtfyConfig{
			Enabled:   true,
			UUID:      "test-uuid-1234",
			AlertInfo: true,
			AlertWarn: true,
			AlertAlarm: true,
		}, handler)

		client.Send(tt.level, "Test Title", "test message")
		server.Close()

		mu.Lock()
		if captured == nil {
			t.Errorf("level %q: no request captured", tt.level)
			mu.Unlock()
			continue
		}

		if captured.Method != "POST" {
			t.Errorf("level %q: method = %q, want POST", tt.level, captured.Method)
		}
		if captured.URL != "/test-uuid-1234" {
			t.Errorf("level %q: URL = %q, want /test-uuid-1234", tt.level, captured.URL)
		}
		if captured.Body != "test message" {
			t.Errorf("level %q: body = %q, want %q", tt.level, captured.Body, "test message")
		}
		if captured.Header.Get("Title") != "Test Title" {
			t.Errorf("level %q: Title header = %q, want %q", tt.level, captured.Header.Get("Title"), "Test Title")
		}
		if captured.Header.Get("Priority") != tt.wantPriority {
			t.Errorf("level %q: Priority header = %q, want %q", tt.level, captured.Header.Get("Priority"), tt.wantPriority)
		}
		if captured.Header.Get("Tags") != tt.wantTags {
			t.Errorf("level %q: Tags header = %q, want %q", tt.level, captured.Header.Get("Tags"), tt.wantTags)
		}
		mu.Unlock()
	}
}

func TestNtfyClientSendWithToken(t *testing.T) {
	var mu sync.Mutex
	var captured *capturedRequest

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		captured = &capturedRequest{
			Method: r.Method,
			URL:    r.URL.String(),
			Body:   string(body),
			Header: r.Header.Clone(),
		}
		w.WriteHeader(http.StatusOK)
	})

	client, server := newTestNtfyClient(t, config.NtfyConfig{
		Enabled:   true,
		UUID:      "test-uuid-1234",
		Token:     "my-secret-token",
		AlertInfo: true,
	}, handler)
	defer server.Close()

	client.Send("info", "Test", "hello")

	mu.Lock()
	defer mu.Unlock()
	if captured == nil {
		t.Fatal("no request captured")
	}
	got := captured.Header.Get("Authorization")
	want := "Bearer my-secret-token"
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

func TestNtfyClientSendWithoutToken(t *testing.T) {
	var mu sync.Mutex
	var captured *capturedRequest

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		captured = &capturedRequest{
			Method: r.Method,
			URL:    r.URL.String(),
			Body:   string(body),
			Header: r.Header.Clone(),
		}
		w.WriteHeader(http.StatusOK)
	})

	client, server := newTestNtfyClient(t, config.NtfyConfig{
		Enabled:   true,
		UUID:      "test-uuid-1234",
		Token:     "",
		AlertInfo: true,
	}, handler)
	defer server.Close()

	client.Send("info", "Test", "hello")

	mu.Lock()
	defer mu.Unlock()
	if captured == nil {
		t.Fatal("no request captured")
	}
	got := captured.Header.Get("Authorization")
	if got != "" {
		t.Errorf("Authorization header = %q, want empty", got)
	}
}

func TestNtfyClientSendTrailingSlash(t *testing.T) {
	var mu sync.Mutex
	var captured *capturedRequest

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		captured = &capturedRequest{
			Method: r.Method,
			URL:    r.URL.String(),
			Body:   string(body),
			Header: r.Header.Clone(),
		}
		w.WriteHeader(http.StatusOK)
	})

	cfg := &config.Config{Ntfy: config.NtfyConfig{
		Enabled:   true,
		UUID:      "test-uuid-1234",
		AlertInfo: true,
	}}
	server := httptest.NewServer(handler)
	defer server.Close()
	cfg.Ntfy.Server = server.URL + "/"
	client := NewNtfyClient(cfg)

	client.Send("info", "Test", "hello")

	mu.Lock()
	defer mu.Unlock()
	if captured == nil {
		t.Fatal("no request captured")
	}
	if captured.URL != "/test-uuid-1234" {
		t.Errorf("URL = %q, want /test-uuid-1234 (double slash avoided)", captured.URL)
	}
}

func TestNtfyClientSendServerError(t *testing.T) {
	var mu sync.Mutex
	var captured *capturedRequest

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		captured = &capturedRequest{
			Method: r.Method,
			URL:    r.URL.String(),
			Body:   string(body),
			Header: r.Header.Clone(),
		}
		w.WriteHeader(http.StatusInternalServerError)
	})

	client, server := newTestNtfyClient(t, config.NtfyConfig{
		Enabled:   true,
		UUID:      "test-uuid-1234",
		AlertInfo: true,
	}, handler)
	defer server.Close()

	client.Send("info", "Test", "hello")

	mu.Lock()
	defer mu.Unlock()
	if captured == nil {
		t.Fatal("no request captured")
	}
}
