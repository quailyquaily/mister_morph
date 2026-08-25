package llmutil

import (
	"context"

	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
	"github.com/quailyquaily/uniai/subscription"
)

type codexSubscriptionSource struct {
	stateDir string
}

func (s codexSubscriptionSource) Credential(ctx context.Context) (subscription.Credential, error) {
	token, err := codexauth.ResolveToken(ctx, s.stateDir, codexauth.OAuthConfig{})
	if err != nil {
		return subscription.Credential{}, err
	}
	return subscription.Credential{AccessToken: token.AccessToken, AccountID: token.AccountID}, nil
}

func (s codexSubscriptionSource) RefreshRejected(ctx context.Context, rejectedAccessToken string) (subscription.Credential, error) {
	token, err := codexauth.RefreshRejectedToken(ctx, s.stateDir, codexauth.OAuthConfig{}, rejectedAccessToken)
	if err != nil {
		return subscription.Credential{}, err
	}
	return subscription.Credential{AccessToken: token.AccessToken, AccountID: token.AccountID}, nil
}

type xaiSubscriptionSource struct {
	stateDir string
}

func (s xaiSubscriptionSource) Credential(ctx context.Context) (subscription.Credential, error) {
	token, err := xaiauth.ResolveToken(ctx, s.stateDir, xaiauth.OAuthConfig{})
	if err != nil {
		return subscription.Credential{}, err
	}
	return subscription.Credential{AccessToken: token.AccessToken}, nil
}

func (s xaiSubscriptionSource) RefreshRejected(ctx context.Context, rejectedAccessToken string) (subscription.Credential, error) {
	token, err := xaiauth.RefreshRejectedToken(ctx, s.stateDir, xaiauth.OAuthConfig{}, rejectedAccessToken)
	if err != nil {
		return subscription.Credential{}, err
	}
	return subscription.Credential{AccessToken: token.AccessToken}, nil
}
