package connector

import (
	"encoding/json"
	"testing"
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
