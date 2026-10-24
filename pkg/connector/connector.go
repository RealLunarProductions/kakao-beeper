package connector

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	up "go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type KakaoConnector struct {
	Bridge *bridgev2.Bridge
	Config Config

	// iconMXC is the uploaded network icon, set in Start (before bridge info is sent).
	iconMXC id.ContentURIString
}

//go:embed kakaotalk-icon.png
var networkIcon []byte

var _ bridgev2.NetworkConnector = (*KakaoConnector)(nil)

type Config struct {
	// NodePath is the node binary used to run the sidecar.
	NodePath string `yaml:"node_path"`
	// SidecarScript is the path to sidecar/sidecar.mjs.
	SidecarScript string `yaml:"sidecar_script"`
	// StateDir is the sidecar's private agent-messenger config dir.
	StateDir string `yaml:"state_dir"`
	// InitialBackfill is how many recent messages to bridge when a chat is first created.
	InitialBackfill int `yaml:"initial_backfill"`
}

//go:embed example-config.yaml
var ExampleConfig string

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "node_path")
	helper.Copy(up.Str, "sidecar_script")
	helper.Copy(up.Str, "state_dir")
	helper.Copy(up.Int, "initial_backfill")
}

func (kc *KakaoConnector) GetConfig() (example string, data any, upgrader up.Upgrader) {
	return ExampleConfig, &kc.Config, &up.StructUpgrader{
		SimpleUpgrader: up.SimpleUpgrader(upgradeConfig),
		Base:           ExampleConfig,
	}
}

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func (kc *KakaoConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "KakaoTalk",
		NetworkURL:           "https://www.kakaocorp.com/page/service/service/KakaoTalk",
		NetworkIcon:          kc.iconMXC,
		NetworkID:            "kakaotalk",
		BeeperBridgeType:     "kakaotalk",
		DefaultPort:          29380,
		DefaultCommandPrefix: "!kakao",
	}
}

func (kc *KakaoConnector) Init(bridge *bridgev2.Bridge) {
	kc.Bridge = bridge
	kc.Config.NodePath = expandHome(kc.Config.NodePath)
	kc.Config.SidecarScript = expandHome(kc.Config.SidecarScript)
	kc.Config.StateDir = expandHome(kc.Config.StateDir)
	if kc.Config.InitialBackfill <= 0 {
		kc.Config.InitialBackfill = 30
	}
}

func (kc *KakaoConnector) Start(ctx context.Context) error {
	kc.iconMXC = kc.uploadNetworkIcon(ctx)
	return nil
}

// uploadNetworkIcon uploads the embedded KakaoTalk logo once and caches its mxc URI
// (keyed by the image hash) next to the sidecar state.
func (kc *KakaoConnector) uploadNetworkIcon(ctx context.Context) id.ContentURIString {
	log := zerolog.Ctx(ctx)
	sum := sha256.Sum256(networkIcon)
	hash := hex.EncodeToString(sum[:8])
	cachePath := filepath.Join(filepath.Dir(kc.Config.StateDir), "network-icon.txt")
	if cached, err := os.ReadFile(cachePath); err == nil {
		if h, uri, ok := strings.Cut(strings.TrimSpace(string(cached)), " "); ok && h == hash {
			return id.ContentURIString(uri)
		}
	}
	uri, _, err := kc.Bridge.Bot.UploadMedia(ctx, "", networkIcon, "kakaotalk.png", "image/png")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to upload network icon")
		return ""
	}
	if err = os.WriteFile(cachePath, []byte(hash+" "+string(uri)+"\n"), 0o600); err != nil {
		log.Warn().Err(err).Msg("Failed to cache network icon URI")
	}
	log.Info().Str("mxc", string(uri)).Msg("Uploaded network icon")
	return uri
}

var generalCaps = &bridgev2.NetworkGeneralCapabilities{}

func (kc *KakaoConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return generalCaps
}

// Bump info when the bridge info content changes so existing rooms get it resent
// (2: network icon, 3: m.bridge network section).
func (kc *KakaoConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 3, 1
}

var _ bridgev2.PortalBridgeInfoFillingNetwork = (*KakaoConnector)(nil)

// FillPortalBridgeInfo adds a network section to m.bridge. Beeper Desktop has no
// built-in KakaoTalk platform (it shows "Generic Matrix Bridge"), but it draws
// network.avatar_url as the account icon and uses network.displayname in labels.
func (kc *KakaoConnector) FillPortalBridgeInfo(portal *bridgev2.Portal, content *event.BridgeEventContent) {
	content.Protocol.DisplayName = "KakaoTalk"
	content.Network = &event.BridgeInfoSection{
		ID:          "kakaotalk",
		DisplayName: "KakaoTalk",
		AvatarURL:   kc.iconMXC,
	}
}

func (kc *KakaoConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{}
}

func (kc *KakaoConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	login.Client = &KakaoClient{
		Main:      kc,
		UserLogin: login,
		members:   make(map[string]KakaoMember),
	}
	return nil
}
