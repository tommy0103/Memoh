package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscoverSelfVerifiesChangedSecretDespiteCachedToken(t *testing.T) {
	// Do not run in parallel: intercept the SDK's default HTTP transport without
	// exposing an endpoint override in production credentials.
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	tokenRequests, infoRequests := 0, 0
	http.DefaultTransport = discoveryTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case larkcore.TenantAccessTokenInternalUrlPath:
			tokenRequests++
			var req larkcore.SelfBuiltTenantAccessTokenReq
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				return nil, err
			}
			switch req.AppSecret {
			case "valid":
				body = fmt.Sprintf(`{"code":0,"tenant_access_token":"token-%d","expire":7200}`, tokenRequests)
			case "empty":
				body = `{"code":0,"tenant_access_token":"","expire":7200}`
			default:
				body = `{"code":10014,"msg":"invalid secret"}`
			}
		case "/open-apis/bot/v3/info":
			infoRequests++
			if got, want := r.Header.Get("Authorization"), fmt.Sprintf("Bearer token-%d", tokenRequests); got != want {
				t.Errorf("identity request token = %q, want fresh token %q", got, want)
			}
			body = `{"code":0,"bot":{"open_id":"ou_test"}}`
		default:
			return nil, fmt.Errorf("unexpected request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	cfg := Config{AppID: uuid.NewString(), AppSecret: "valid", Region: regionFeishu}
	// A running connection has already cached a token for this App ID.
	if _, err := cfg.newClient().Get(context.Background(), "/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant); err != nil {
		t.Fatal(err)
	}
	adapter := &FeishuAdapter{}
	for _, tc := range []struct {
		secret string
		fail   bool
	}{{"wrong", true}, {"empty", true}, {"valid", false}} {
		t.Run(tc.secret, func(t *testing.T) {
			beforeToken, beforeInfo := tokenRequests, infoRequests
			identity, id, err := adapter.DiscoverSelf(context.Background(), map[string]any{"appId": cfg.AppID, "appSecret": tc.secret})
			if (err != nil) != tc.fail {
				t.Fatalf("error = %v, want failure %v", err, tc.fail)
			}
			if tokenRequests != beforeToken+1 {
				t.Fatalf("token requests = %d, want %d", tokenRequests, beforeToken+1)
			}
			if tc.fail {
				if infoRequests != beforeInfo {
					t.Fatal("queried identity after failed credential verification")
				}
			} else if id != "ou_test" || identity["open_id"] != id {
				t.Fatalf("unexpected identity: %v, %q", identity, id)
			}
		})
	}
}
