package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	connectsdk "github.com/felinics/connect-it/sdk/go"

	"github.com/felinics/memoh/internal/config"
	mcpgw "github.com/felinics/memoh/internal/mcp"
)

func TestToolDiscoveryStatusAnnotatesSessionWideFailure(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	svc.RecordToolDiscoveryFailure("bot-1", &connectsdk.APIError{
		StatusCode: http.StatusBadGateway,
		Code:       "tool_discovery_failed",
		Message:    "failed to discover tools for the requested connections",
	})

	items := []Connector{
		{Alias: "gmail", Enabled: true, Status: "active"},
		{Alias: "notion", Enabled: false, Status: "active"},
		{Alias: "github", Enabled: true, Status: "reauth_required"},
	}
	svc.annotateToolDiscovery("bot-1", items)
	want := "tool_discovery_failed: failed to discover tools for the requested connections"
	if items[0].ToolDiscoveryError != want {
		t.Fatalf("enabled active connection must carry the error, got %q", items[0].ToolDiscoveryError)
	}
	if items[1].ToolDiscoveryError != "" || items[2].ToolDiscoveryError != "" {
		t.Fatalf("connections outside the session must stay clean: %+v", items[1:])
	}
	status, ok := svc.ToolDiscoveryStatus("bot-1")
	if !ok || status.Error != want || !status.Degraded() || status.ObservedAt.IsZero() {
		t.Fatalf("status=%+v ok=%v", status, ok)
	}
	if lines := svc.ToolDiagnostics("bot-1"); len(lines) != 1 || !strings.Contains(lines[0], want) ||
		!strings.Contains(lines[0], "do not claim that no connector exists") {
		t.Fatalf("diagnostics=%q", lines)
	}

	svc.RecordToolDiscoverySuccess("bot-1")
	if status, _ := svc.ToolDiscoveryStatus("bot-1"); status.Degraded() {
		t.Fatalf("success must clear the session-wide error: %+v", status)
	}
	if lines := svc.ToolDiagnostics("bot-1"); lines != nil {
		t.Fatalf("healthy status must not produce diagnostics: %q", lines)
	}
}

func TestToolDiscoveryStatusAnnotatesPerConnectionFailure(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	svc.RecordToolDiscoverySession("bot-1", []ToolDiscoveryFailure{
		{Alias: "gmail", ConnectionID: "c-1", Error: "tools/list: Forbidden"},
	})
	svc.RecordToolDiscoverySuccess("bot-1")

	items := []Connector{
		{Alias: "gmail", Enabled: true, Status: "active"},
		{Alias: "github", Enabled: true, Status: "active"},
	}
	svc.annotateToolDiscovery("bot-1", items)
	if items[0].ToolDiscoveryError != "tools/list: Forbidden" || items[1].ToolDiscoveryError != "" {
		t.Fatalf("items=%+v", items)
	}
	lines := svc.ToolDiagnostics("bot-1")
	if len(lines) != 1 || !strings.Contains(lines[0], `"gmail"`) || !strings.Contains(lines[0], "other connector tools are available") {
		t.Fatalf("diagnostics=%q", lines)
	}

	svc.ClearToolDiscovery("bot-1")
	if _, ok := svc.ToolDiscoveryStatus("bot-1"); ok {
		t.Fatal("clear must forget the status")
	}
	var nilService *Service
	nilService.RecordToolDiscoveryFailure("bot-1", errors.New("x"))
	if lines := nilService.ToolDiagnostics("bot-1"); lines != nil {
		t.Fatalf("nil service must be inert, got %q", lines)
	}
}

func TestDiscoveryErrorTextBoundsTransportErrors(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxDiscoveryErrorLen+50)
	if got := discoveryErrorText(errors.New(long)); len([]rune(got)) != maxDiscoveryErrorLen+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long error must be truncated, got len=%d", len(got))
	}
	if got := discoveryErrorText(errors.New("  dial tcp: refused ")); got != "dial tcp: refused" {
		t.Fatalf("got %q", got)
	}
}

func TestSourceReportsBackoffCauseInsteadOfEmptyList(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	source := NewSource(nil, svc)
	cause := errors.New("connect-it: tool_discovery_failed (HTTP 502)")
	source.noteListFailure("bot-1", cause)

	tools, err := source.ListTools(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"})
	if !errors.Is(err, cause) || tools != nil {
		t.Fatalf("backoff must surface the cause, got tools=%v err=%v", tools, err)
	}
	if lines := source.ToolDiagnostics(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"}); len(lines) != 1 ||
		!strings.Contains(lines[0], cause.Error()) {
		t.Fatalf("diagnostics=%q", lines)
	}

	source.Invalidate("bot-1")
	if _, backingOff := source.inFailureBackoff("bot-1"); backingOff {
		t.Fatal("invalidate must end the backoff")
	}
	if _, ok := svc.ToolDiscoveryStatus("bot-1"); ok {
		t.Fatal("invalidate must clear the recorded status")
	}

	source.mu.Lock()
	source.retryAfter["bot-2"] = listFailure{until: time.Now().Add(-time.Second), err: cause}
	source.mu.Unlock()
	if _, backingOff := source.inFailureBackoff("bot-2"); backingOff {
		t.Fatal("an expired window must not back off")
	}
}

func TestSourceRecordsConnectionsExcludedFromIssuedSession(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcp-sessions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "session-1",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"failed_connections": []map[string]any{{
				"alias":         "cloudflare",
				"connection_id": "conn-cf",
				"error":         "  mcpclient: initialize: Unauthorized ",
			}},
		})
	}))
	t.Cleanup(upstream.Close)
	svc, err := NewService(nil, config.Config{ConnectIt: config.ConnectItConfig{
		BaseURL: upstream.URL, APIToken: "cit_test",
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := NewSource(nil, svc)

	handler := source.authHandler("bot-1", map[string]string{"cloudflare": "conn-cf", "resend": "conn-rs"})
	if _, err := handler.TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, ok := svc.ToolDiscoveryStatus("bot-1")
	if !ok || len(status.FailedConnections) != 1 ||
		status.FailedConnections[0] != (ToolDiscoveryFailure{Alias: "cloudflare", ConnectionID: "conn-cf", Error: "mcpclient: initialize: Unauthorized"}) {
		t.Fatalf("status=%+v ok=%v", status, ok)
	}
	items := []Connector{
		{Alias: "cloudflare", Enabled: true, Status: "active"},
		{Alias: "resend", Enabled: true, Status: "active"},
	}
	svc.annotateToolDiscovery("bot-1", items)
	if items[0].ToolDiscoveryError == "" || items[1].ToolDiscoveryError != "" {
		t.Fatalf("items=%+v", items)
	}
	if lines := source.ToolDiagnostics(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"}); len(lines) != 1 ||
		!strings.Contains(lines[0], `"cloudflare"`) {
		t.Fatalf("diagnostics=%q", lines)
	}
}
