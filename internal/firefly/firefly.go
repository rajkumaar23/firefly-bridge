package firefly

import (
	"context"
	"fmt"
	"net/http"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"
)

func NewFireflyClient(host, token string) (*ClientWithResponses, error) {
	ffToken, err := securityprovider.NewSecurityProviderBearerToken(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create security provider: %w", err)
	}
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ff, err := NewClientWithResponses(
		host,
		WithHTTPClient(client),
		WithRequestEditorFn(ffToken.Intercept),
		WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Accept", "application/json")
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create firefly client: %v", err)
	}
	return ff, nil
}
