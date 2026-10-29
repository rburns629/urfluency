package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/report"
	"github.com/rburns629/urfluency/upload"
)

// Uploading is optional and never part of a scan. `urfluency login` links the
// CLI to an account with a device flow (RFC 8628): the API issues a short code,
// the user approves it on the website while signed in, and the CLI receives an
// upload-only token. `urfluency upload` scans, shows what it will send, and asks
// before sending.

const defaultAPI = "https://api.urfluency.com"

// apiBase is the API to talk to: URFLUENCY_API_URL, or the public API. Plain
// HTTP is allowed only for a local development server.
func apiBase() (string, error) {
	base := os.Getenv("URFLUENCY_API_URL")
	if base == "" {
		return defaultAPI, nil
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("URFLUENCY_API_URL %q is not a URL", base)
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", fmt.Errorf("URFLUENCY_API_URL must use https, except for localhost")
	}
	return strings.TrimRight(base, "/"), nil
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// credentials are the upload token `urfluency login` saved. They stay on this
// machine, readable only by the user, and are never printed.
type credentials struct {
	API     string    `json:"api"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires_at"`
}

func credentialsPath() (string, error) {
	settings, err := settingsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(settings), "credentials.json"), nil
}

func loadCredentials() (credentials, error) {
	var c credentials
	path, err := credentialsPath()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, fmt.Errorf("not logged in; run urfluency login")
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("read credentials: %w", err)
	}
	if time.Now().After(c.Expires) {
		return c, fmt.Errorf("your login expired; run urfluency login")
	}
	return c, nil
}

// save writes the credentials atomically with owner-only permissions.
func (c credentials) save() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// apiError is the API's error body.
type apiError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// postJSON sends body as JSON and decodes a JSON response into out when the
// status is 2xx. Otherwise it returns the status and the API's error code.
func postJSON(ctx context.Context, endpoint, token string, body, out any) (int, string, error) {
	return postJSONWith(ctx, endpoint, token, nil, body, out)
}

// postJSONWith is postJSON with extra request headers.
func postJSONWith(ctx context.Context, endpoint, token string, headers map[string]string, body, out any) (int, string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "urfluency/"+Version)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode/100 != 2 {
		var e apiError
		_ = json.Unmarshal(raw, &e)
		return resp.StatusCode, e.Error, nil
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, "", fmt.Errorf("unexpected response from %s: %w", endpoint, err)
		}
	}
	return resp.StatusCode, "", nil
}

// deviceStart is the API's answer to a device login request.
type deviceStart struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type deviceToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// pollInterval is the shortest wait between token requests; tests lower it.
var pollInterval = 5 * time.Second

func login(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return fmt.Errorf("%w: login takes no arguments", errUsage)
	}
	base, err := apiBase()
	if err != nil {
		return err
	}
	var start deviceStart
	status, code, err := postJSON(ctx, base+"/v1/auth/device", "", map[string]string{"client": "urfluency-cli", "version": Version}, &start)
	if err != nil {
		return fmt.Errorf("start login: %w", err)
	}
	if status/100 != 2 {
		return fmt.Errorf("start login: %s (%d)", code, status)
	}
	link := start.VerificationURIComplete
	if link == "" {
		link = start.VerificationURI
	}
	fmt.Fprintf(stdout, "Open %s\nand approve this code after signing in: %s\n\nWaiting for approval…\n", link, start.UserCode)

	interval := max(time.Duration(start.Interval)*time.Second, pollInterval)
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		var tok deviceToken
		status, code, err := postJSON(ctx, base+"/v1/auth/device/token", "", map[string]string{"device_code": start.DeviceCode}, &tok)
		if err != nil {
			return fmt.Errorf("finish login: %w", err)
		}
		switch {
		case status/100 == 2 && tok.AccessToken != "":
			c := credentials{API: base, Token: tok.AccessToken, Expires: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)}
			if err := c.save(); err != nil {
				return fmt.Errorf("save login: %w", err)
			}
			fmt.Fprintf(stdout, "Logged in. Uploads are allowed until %s; run urfluency upload when you want to share results.\n", c.Expires.Format("2006-01-02"))
			return nil
		case code == "authorization_pending":
		case code == "slow_down":
			interval += 5 * time.Second
		case code == "access_denied":
			return errors.New("login was denied on the website")
		case code == "expired_token":
			return errors.New("the code expired; run urfluency login again")
		default:
			return fmt.Errorf("finish login: %s (%d)", code, status)
		}
	}
	return errors.New("the code expired; run urfluency login again")
}

func logout(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: logout takes no arguments", errUsage)
	}
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	fmt.Fprintln(stdout, "Logged out. The upload token on this machine was deleted.")
	return nil
}

// buildPayload takes only the summary and the combined result from a report:
// the parts written for others to read, without locations or repository names.
func buildPayload(r report.Report) (upload.Payload, error) {
	if r.Summary == nil || r.Combined == nil {
		return upload.Payload{}, errors.New("the report has no summary to upload")
	}
	summary, err := json.Marshal(r.Summary)
	if err != nil {
		return upload.Payload{}, err
	}
	combined, err := json.Marshal(r.Combined)
	if err != nil {
		return upload.Payload{}, err
	}
	tier := upload.SelfReported
	if r.Repos == 0 {
		tier = upload.Declared // nothing in the result came from Git history
	}
	p := upload.Payload{Format: upload.Format, Tool: Version, Report: r.Format, Catalog: r.Catalog, Model: report.ProficiencyModel,
		Tier: tier, Since: r.Since, Summary: summary, Combined: combined}
	return p, upload.Validate(p)
}

// describePayload says in a few lines what an upload contains.
func describePayload(r report.Report) string {
	counts := map[string]int{}
	for _, rt := range r.Summary.Ratings {
		counts[rt.Area]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This upload contains:\n  %s\n", r.Summary.Headline)
	fmt.Fprintf(&b, "  ratings: %s, %s, %s, %s, %s", count(counts["language"], "language"), count(counts["framework"], "framework"),
		count(counts["design"], "design area"), count(counts["tool"], "tool"), count(counts["practice"], "practice"))
	if counts["prompting"] > 0 {
		b.WriteString(", prompting")
	}
	fmt.Fprintf(&b, "\n  %s of strengths, %s holding ratings down, and caveats\n", count(len(r.Summary.Strengths), "line"), count(len(r.Summary.Growth), "line"))
	b.WriteString("It does not contain code, prompt text, repository names, paths, commit hashes, or email addresses.\n")
	b.WriteString("See every field with: urfluency upload --dry-run [same flags]\n")
	return b.String()
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

type uploadResult struct {
	ProfileURL string `json:"profile_url"`
}

func uploadCmd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sf := addScanFlags(fs)
	yes := fs.Bool("yes", false, "")
	dryRun := fs.Bool("dry-run", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	path, err := onePath(fs, "upload")
	if err != nil {
		return err
	}
	var creds credentials
	if !*dryRun {
		if creds, err = loadCredentials(); err != nil {
			return err
		}
	}
	fmt.Fprintln(stderr, "Scanning again: upload builds its own report and does not reuse an earlier scan.")
	r, err := buildReport(ctx, sf, path, false, stderr)
	if err != nil {
		return err
	}
	p, err := buildPayload(r)
	if err != nil {
		return fmt.Errorf("not uploading: %w", err)
	}
	if *dryRun {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	fmt.Fprint(stdout, describePayload(r))
	if !*yes {
		if !interactive() {
			return fmt.Errorf("%w: confirm with --yes when not running in a terminal", errUsage)
		}
		fmt.Fprint(stdout, "Send it? [y/N] ")
		in := bufio.NewScanner(stdin)
		if !in.Scan() || !strings.EqualFold(strings.TrimSpace(in.Text()), "y") && !strings.EqualFold(strings.TrimSpace(in.Text()), "yes") {
			fmt.Fprintln(stdout, "Nothing was sent.")
			return nil
		}
	}
	var res uploadResult
	status, code, err := postJSON(ctx, creds.API+"/v1/metrics", creds.Token, p, &res)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	switch {
	case status == http.StatusUnauthorized:
		return errors.New("the API did not accept your login; run urfluency login")
	case status/100 != 2:
		return fmt.Errorf("upload: %s (%d)", code, status)
	}
	fmt.Fprintln(stdout, "Uploaded. Metrics stay private until you publish them.")
	if res.ProfileURL != "" {
		fmt.Fprintln(stdout, "Your profile:", res.ProfileURL)
	}
	return nil
}
