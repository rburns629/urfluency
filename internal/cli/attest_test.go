package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/internal/testutil"
	"github.com/rburns629/urfluency/upload"
)

// attest asks GitHub for a token for the API's audience and sends the
// ratings with it, and with the link code from the environment.
func TestAttestSendsRatingsWithTheJobToken(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Commit("a@example.com", "2026-09-01T00:00:00Z", map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	var gotAudience, gotAuth, gotLink string
	var gotPayload upload.Payload
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/repos/attest" {
			http.NotFound(w, r)
			return
		}
		gotAuth, gotLink = r.Header.Get("Authorization"), r.Header.Get("X-Urfluency-Link-Code")
		json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Write([]byte(`{"repository":"acme/app","badges_url":"https://urfluency.test/account/repos"}`))
	}))
	defer api.Close()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer request-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		gotAudience = r.URL.Query().Get("audience")
		w.Write([]byte(`{"value":"job-oidc-token"}`))
	}))
	defer gh.Close()

	t.Setenv("URFLUENCY_API_URL", api.URL)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", gh.URL+"/token?api-version=2.0")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "request-token")
	t.Setenv("GITHUB_WORKSPACE", repo.Dir)
	t.Setenv("URFLUENCY_LINK_CODE", "ufl_link_abc")
	var out bytes.Buffer
	if err := attest(context.Background(), nil, &out); err != nil {
		t.Fatal(err)
	}
	if gotAudience != api.URL || gotAuth != "Bearer job-oidc-token" || gotLink != "ufl_link_abc" {
		t.Errorf("audience %q, authorization %q, link code %q", gotAudience, gotAuth, gotLink)
	}
	if err := upload.Validate(gotPayload); err != nil {
		t.Errorf("sent payload: %v", err)
	}
	if !strings.Contains(out.String(), "acme/app") {
		t.Errorf("output = %q", out.String())
	}
}
