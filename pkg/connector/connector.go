package connector

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"

	up "go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

type KakaoConnector struct {
	Bridge *bridgev2.Bridge
	Config Config
}

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
	return nil
}

var generalCaps = &bridgev2.NetworkGeneralCapabilities{}

func (kc *KakaoConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return generalCaps
}

func (kc *KakaoConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 1
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
