package connector

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestKakaoMessageKind(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		kind int
	}{
		{
			name: "live push",
			raw:  `{"type":"MSG","chat_id":"100000000000001","log_id":"100000000000002","author_id":1234,"author_name":null,"message":"","message_type":2,"attachment":{"url":"https://talk.kakaocdn.net/x.jpg","w":640,"h":480},"sent_at":1792800000}`,
			kind: typePhoto,
		},
		{
			name: "history entry",
			raw:  `{"log_id":"100000000000003","type":1,"author_id":100000001,"author_name":null,"message":"hi","attachment":null,"sent_at":1792653885}`,
			kind: typeText,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var msg KakaoMessage
			if err := json.Unmarshal([]byte(tc.raw), &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := msg.Kind(); got != tc.kind {
				t.Fatalf("Kind() = %d, want %d", got, tc.kind)
			}
		})
	}
}

func TestIsAuthError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&SidecarError{Code: "invalid_access_token", Message: "LOGINLIST invalid access token (status -950)"}, true},
		{&SidecarError{Code: "login_rejected", Message: "rejected"}, true},
		{&SidecarError{Code: "error", Message: "No KakaoTalk credentials found. Run: agent-kakaotalk auth login"}, true},
		{&SidecarError{Code: "login_failed", Message: "KakaoTalk LOGINLIST transport failed: connection closed"}, false},
		{&SidecarError{Code: "error", Message: "getaddrinfo ENOTFOUND booking-loco.kakao.com"}, false},
		{fmt.Errorf("wrapped: %w", &SidecarError{Code: "missing_token", Message: "OAuth token is required"}), true},
		{ErrSidecarExited, false},
	}
	for _, tc := range cases {
		if got := isAuthError(tc.err); got != tc.want {
			t.Errorf("isAuthError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestRestartDelay(t *testing.T) {
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 5 * time.Minute, 5 * time.Minute}
	for failures, d := range want {
		if got := restartDelay(failures); got != d {
			t.Errorf("restartDelay(%d) = %v, want %v", failures, got, d)
		}
	}
	if got := restartDelay(100); got != 5*time.Minute {
		t.Errorf("restartDelay(100) = %v, want 5m", got)
	}
}
