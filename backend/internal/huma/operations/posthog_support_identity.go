package operations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

// PostHogSupportIdentityHandler signs the authenticated Clerk subject for
// PostHog Support. The secret token never crosses the API boundary.
type PostHogSupportIdentityHandler struct {
	projectToken string
	secretToken  string
}

type GetPostHogSupportIdentityInput struct {
	inputs.AuthenticatedInput
}

type PostHogSupportIdentity struct {
	DistinctID   string `json:"distinct_id" doc:"Canonical authenticated Clerk user ID"`
	IdentityHash string `json:"identity_hash" doc:"Short-lived transport of the server-computed PostHog identity signature"`
}

type GetPostHogSupportIdentityOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         PostHogSupportIdentity
}

func NewPostHogSupportIdentityHandler(projectToken, secretToken string) *PostHogSupportIdentityHandler {
	return &PostHogSupportIdentityHandler{projectToken: projectToken, secretToken: secretToken}
}

func computePostHogIdentityHash(distinctID, secretToken string) string {
	hash := hmac.New(sha256.New, []byte(secretToken))
	_, _ = hash.Write([]byte(distinctID))
	return hex.EncodeToString(hash.Sum(nil))
}

func (h *PostHogSupportIdentityHandler) Get(_ context.Context, input *GetPostHogSupportIdentityInput) (*GetPostHogSupportIdentityOutput, error) {
	if h.projectToken == "" || h.secretToken == "" {
		return nil, huma.Error503ServiceUnavailable("Support is unavailable")
	}
	return &GetPostHogSupportIdentityOutput{
		CacheControl: "no-store",
		Body: PostHogSupportIdentity{
			DistinctID:   input.UserID,
			IdentityHash: computePostHogIdentityHash(input.UserID, h.secretToken),
		},
	}, nil
}

func RegisterPostHogSupportIdentityRoutes(api huma.API, handler *PostHogSupportIdentityHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getPostHogSupportIdentity",
		Method:      http.MethodGet,
		Path:        "/api/v1/user/posthog-support-identity",
		Summary:     "Get a verified PostHog Support identity",
		Description: "Returns the authenticated Clerk subject and its server-generated HMAC for PostHog Support.",
		Tags:        []string{"Support"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.Get)
}
