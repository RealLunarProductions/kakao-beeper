package connector

import (
	"context"
	"fmt"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/status"
)

// The KakaoTalk account is authenticated outside the bridge with
// `agent-kakaotalk auth login` (password + phone passcode never pass through
// Matrix); this flow just adopts those stored credentials.
var loginFlows = []bridgev2.LoginFlow{{
	Name:        "agent-kakaotalk credentials",
	Description: "Use the tablet-slot session created with `agent-kakaotalk auth login` on this machine",
	ID:          "agent-kakaotalk",
}}

func (kc *KakaoConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return loginFlows
}

func (kc *KakaoConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	if flowID != "agent-kakaotalk" {
		return nil, bridgev2.ErrInvalidLoginFlowID
	}
	return &CredentialLogin{Main: kc, User: user}, nil
}

type CredentialLogin struct {
	Main *KakaoConnector
	User *bridgev2.User
}

var _ bridgev2.LoginProcess = (*CredentialLogin)(nil)

func (cl *CredentialLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	// Only one LOCO connection per device may exist, so probe with a short-lived
	// sidecar and stop it before the real client connects.
	sc, err := StartSidecar(&cl.Main.Config, cl.User.Log, nil)
	if err != nil {
		return nil, err
	}
	var profile KakaoProfile
	err = sc.Call(ctx, "start", nil, &profile)
	sc.Stop()
	if err != nil {
		return nil, fmt.Errorf("failed to connect with the stored KakaoTalk credentials (sign in first: `agent-kakaotalk auth login`, or `kakao-beeper login-kakao` on macOS): %w", err)
	}
	remoteName := profile.Nickname
	if remoteName == "" {
		remoteName = "KakaoTalk " + profile.UserID
	}
	ul, err := cl.User.NewLogin(ctx, &database.UserLogin{
		ID:            makeUserLoginID(profile.UserID),
		RemoteName:    remoteName,
		RemoteProfile: status.RemoteProfile{Name: remoteName},
	}, &bridgev2.NewLoginParams{DeleteOnConflict: true})
	if err != nil {
		return nil, fmt.Errorf("failed to save login: %w", err)
	}
	go ul.Client.Connect(ul.Log.WithContext(context.Background()))
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "kakaotalk.complete",
		Instructions: "Connected to KakaoTalk as " + remoteName,
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}

func (cl *CredentialLogin) Cancel() {}
