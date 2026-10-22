package wechatoa

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/channel"
)

func TestBuildSendPayload_ImagePlatformKey(t *testing.T) {
	client := &apiClient{}
	payload, err := client.buildSendPayload(context.Background(), channel.PreparedMessage{
		Message: channel.Message{
			Attachments: []channel.Attachment{
				{Type: channel.AttachmentImage, PlatformKey: "mid_123"},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildSendPayload error = %v", err)
	}
	if payload["msgtype"] != "image" {
		t.Fatalf("unexpected msgtype: %v", payload["msgtype"])
	}
}

func TestBuildSendPayload_UnsupportedAttachment(t *testing.T) {
	client := &apiClient{}
	_, err := client.buildSendPayload(context.Background(), channel.PreparedMessage{
		Message: channel.Message{
			Attachments: []channel.Attachment{
				{Type: channel.AttachmentFile, PlatformKey: "mid_file"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

type tokenTestTransport func(*http.Request) (*http.Response, error)

func (f tokenTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDiscoverSelfRejectsInvalidTokenResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"platform error", http.StatusOK, `{"errcode":40001,"errmsg":"invalid credential"}`},
		{"error with token", http.StatusOK, `{"errcode":40001,"access_token":"invalid"}`},
		{"missing token", http.StatusOK, `{}`},
		{"null token", http.StatusOK, `{"access_token":null}`},
		{"blank token", http.StatusOK, `{"access_token":"  "}`},
		{"wrong token type", http.StatusOK, `{"access_token":123}`},
		{"HTTP failure", http.StatusUnauthorized, `{"access_token":"invalid"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter := NewWeChatOAAdapter(nil)
			credentials := map[string]any{"appId": "test-app", "appSecret": "secret", "token": "webhook-token", "encryptionMode": "plain"}
			client, err := adapter.clientForConfig(credentials)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client.http = &http.Client{Transport: tokenTestTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				status, body := tc.status, tc.body
				if calls > 1 {
					status, body = http.StatusOK, `{"access_token":"valid-token","expires_in":7200}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			if _, _, err := adapter.DiscoverSelf(context.Background(), credentials); err == nil {
				t.Fatal("invalid token response passed verification")
			}
			if client.tokenCache != "" || !client.expiresAt.IsZero() {
				t.Fatal("invalid response was cached")
			}
			identity, id, err := adapter.DiscoverSelf(context.Background(), credentials)
			if err != nil || id != "test-app" || identity["app_id"] != id {
				t.Fatalf("retry failed: identity=%v id=%q error=%v", identity, id, err)
			}
			if _, _, err := adapter.DiscoverSelf(context.Background(), credentials); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("token calls = %d, want 2 with successful token cached", calls)
			}
		})
	}
}
