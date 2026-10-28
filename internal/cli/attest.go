package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/rburns629/urfluency/score"
)

// attest scores the repository a GitHub Actions job checked out, every
// author's code, and sends the ratings to urFluency with the job's OIDC token:
// the repository's badges, in the CI-attested tier. The code never leaves the
// runner; only the summary and combined result do, as in an upload. A link
// code in URFLUENCY_LINK_CODE connects a repository not yet connected, without
// the GitHub App; it is read from the environment so it stays off the command
// line.
func attest(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("attest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	since := fs.String("since", "365d", "")
	dryRun := fs.Bool("dry-run", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: attest takes no path; it scores the checked-out repository", errUsage)
	}
	from, err := parseSince(*since, time.Now())
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	reqURL, reqToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL"), os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if !*dryRun && (reqURL == "" || reqToken == "") {
		return errors.New("attest runs in GitHub Actions, in a job with `permissions: id-token: write`")
	}
	base, err := apiBase()
	if err != nil {
		return err
	}
	dir := os.Getenv("GITHUB_WORKSPACE")
	if dir == "" {
		dir = "."
	}
	p, err := score.Repo(ctx, dir, from, Version)
	if err != nil {
		return fmt.Errorf("score the repository: %w", err)
	}
	if *dryRun {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	token, err := oidcToken(ctx, reqURL, reqToken, base)
	if err != nil {
		return err
	}
	var out struct {
		Repository string `json:"repository"`
		Badges     string `json:"badges_url"`
	}
	headers := map[string]string{}
	if link := os.Getenv("URFLUENCY_LINK_CODE"); link != "" {
		headers["X-Urfluency-Link-Code"] = link
	}
	status, code, err := postJSONWith(ctx, base+"/v1/repos/attest", token, headers, p, &out)
	if err != nil {
		return fmt.Errorf("send the ratings: %w", err)
	}
	if status/100 != 2 {
		return fmt.Errorf("urFluency refused the ratings: %s (%d)", code, status)
	}
	_, err = fmt.Fprintf(stdout, "Sent %s's ratings (CI-attested). Badges: %s\n", out.Repository, out.Badges)
	return err
}

// oidcToken asks GitHub Actions for an ID token for audience. GitHub signs it;
// it names the repository, workflow, and ref, which urFluency checks.
func oidcToken(ctx context.Context, reqURL, reqToken, audience string) (string, error) {
	u, err := url.Parse(reqURL)
	if err != nil {
		return "", errors.New("ACTIONS_ID_TOKEN_REQUEST_URL is not a URL")
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+reqToken)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("get a GitHub OIDC token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get a GitHub OIDC token: %s", resp.Status)
	}
	var t struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&t); err != nil || t.Value == "" {
		return "", errors.New("get a GitHub OIDC token: no token in the response")
	}
	return t.Value, nil
}
