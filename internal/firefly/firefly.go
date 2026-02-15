package firefly

import (
	"context"
	"fmt"
	"net/http"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"
)

func (ff *ClientWithResponses) VerifyConnection(ctx context.Context) error {
	ffSysInfo, err := ff.GetAboutWithResponse(ctx, &GetAboutParams{})
	if err != nil {
		return err
	}
	if ffSysInfo.JSON200 == nil {
		return fmt.Errorf("%s", ffSysInfo.Status())
	}
	return nil
}

func NewFireflyClient(ctx context.Context, host, token string) (*ClientWithResponses, error) {
	ffToken, err := securityprovider.NewSecurityProviderBearerToken(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create security provider: %w", err)
	}
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ff, err := NewClientWithResponses(
		host,
		WithHTTPClient(client),
		WithRequestEditorFn(ffToken.Intercept),
		WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Accept", "application/json")
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create firefly client: %v", err)
	}

	err = ff.VerifyConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to verify connection to firefly: %w", err)
	}

	return ff, nil
}
