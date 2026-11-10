package connector

import (
	"encoding/json"
	"testing"
)

func TestCallText(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"type":1,"message_type":51,"message":"{ \"type\":\"v_invite\",\"callId\":\"1\" }","attachment":{"type":"v_invite","duration":0}}`, "📞 통화 시작"},
		{`{"type":51,"message":"{ \"type\":\"v_bye\",\"duration\":886 }","attachment":{"type":"v_bye","duration":886}}`, "📞 통화 종료 (14:46)"},
		{`{"type":51,"message":"{ \"type\":\"v_canceled\",\"duration\":0 }","attachment":null}`, "📞 통화 취소"},
	}
	for _, tc := range cases {
		var msg KakaoMessage
		if err := json.Unmarshal([]byte(tc.raw), &msg); err != nil {
			t.Fatal(err)
		}
		if got := callText(&msg, msg.Attachment); got != tc.want {
			t.Errorf("callText(%s) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestParseFeed(t *testing.T) {
	msg := &KakaoMessage{Message: `{"logId":3970000000000000001,"byHost":false,"hidden":true,"feedType":14}`}
	feed := parseFeed(msg)
	if feed == nil || feed.FeedType != feedDeleteToAll || feed.LogID != "3970000000000000001" || !feed.Hidden {
		t.Fatalf("parseFeed = %+v", feed)
	}
	invite := parseFeed(&KakaoMessage{Message: `{"inviter":{"userId":1,"nickName":"민수"},"members":[{"userId":2,"nickName":"지영"}],"byLink":false,"feedType":1}`})
	if got := feedText(invite); got != "민수님이 지영님을 초대했습니다." {
		t.Errorf("feedText = %q", got)
	}
	if parseFeed(&KakaoMessage{Type: json.RawMessage("1"), Message: `{"feedType":1}`}) != nil {
		t.Error("text message parsed as a feed")
	}
}

func TestDeletedFlag(t *testing.T) {
	msg := &KakaoMessage{Type: json.RawMessage("16385")}
	if !msg.Deleted() || msg.BaseKind() != typeText {
		t.Errorf("Deleted=%v BaseKind=%d", msg.Deleted(), msg.BaseKind())
	}
}
