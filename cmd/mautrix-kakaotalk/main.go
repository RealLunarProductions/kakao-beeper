// kakao-beeper: a KakaoTalk bridge for Beeper (mautrix-go bridgev2) backed by
// agent-messenger's LOCO client running in a Node sidecar.
package main

import (
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"

	"github.com/RealLunarProductions/kakao-beeper/pkg/connector"
)

// Filled at build time with -X linker flags.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var m = mxmain.BridgeMain{
	Name:        "kakao-beeper",
	URL:         "https://github.com/RealLunarProductions/kakao-beeper",
	Description: "A Matrix-KakaoTalk puppeting bridge for Beeper.",
	Version:     "0.1.0",
	Connector:   &connector.KakaoConnector{},
}

func main() {
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
