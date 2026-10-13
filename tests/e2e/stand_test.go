//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// stand is the running e2e stand. Its parameters come from make test-e2e.
type stand struct {
	stateDir    string
	composeFile string
	baseURL     string
	sshPort     string
	client      *http.Client
	state       *standState
}

// standState is everything bootstrap creates; it is kept in E2E_STATE_DIR/state.json.
type standState struct {
	RootToken      string                   `json:"root_token"`
	Users          map[string]*standUser    `json:"users"`
	ServiceAccount *standUser               `json:"service_account"`
	Groups         map[string]int           `json:"groups"`
	Projects       map[string]*standProject `json:"projects"`
	RunnerToken    string                   `json:"runner_token"`
}

type standUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token"`
	SSHKey   string `json:"ssh_key,omitempty"`
}

type standProject struct {
	ID          int    `json:"id"`
	Path        string `json:"path"`
	AccessToken string `json:"access_token,omitempty"`
	DeployKey   string `json:"deploy_key,omitempty"`
}

func newStand() (*stand, error) {
	vars := map[string]string{}
	for _, name := range []string{"E2E_STATE_DIR", "E2E_HTTPS_PORT", "E2E_SSH_PORT", "COMPOSE_PROJECT_NAME"} {
		v := os.Getenv(name)
		if v == "" {
			return nil, fmt.Errorf("%s is not set: run the e2e tests through make test-e2e", name)
		}
		vars[name] = v
	}
	stateDir := vars["E2E_STATE_DIR"]
	caPEM, err := os.ReadFile(filepath.Join(stateDir, "tls", "ca.crt"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("no certificates in the stand CA file")
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	s := &stand{
		stateDir:    stateDir,
		composeFile: filepath.Join(wd, "..", "..", "docker-compose.yml"),
		baseURL:     "https://localhost:" + vars["E2E_HTTPS_PORT"],
		sshPort:     vars["E2E_SSH_PORT"],
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
		state: &standState{},
	}
	return s, s.loadState()
}

func (s *stand) statePath() string { return filepath.Join(s.stateDir, "state.json") }

func (s *stand) loadState() error {
	data, err := os.ReadFile(s.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, s.state)
}

func (s *stand) saveState() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.statePath(), append(data, '\n'), 0o600)
}

// apiError is a GitLab API response outside 2xx.
type apiError struct {
	Method, Path string
	Status       int
	Body         string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, e.Body)
}

func isStatus(err error, status int) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == status
}

// api calls the REST API v4 with token; body is sent as JSON, a 2xx response is decoded into out.
func (s *stand) api(ctx context.Context, token, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+"/api/v4"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &apiError{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// root calls the API as the instance administrator.
func (s *stand) root(ctx context.Context, method, path string, body, out any) error {
	return s.api(ctx, s.state.RootToken, method, path, body, out)
}

// tokenActive reports whether token is accepted by the API.
func (s *stand) tokenActive(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	return s.api(ctx, token, http.MethodGet, "/personal_access_tokens/self", nil, nil) == nil
}

// compose runs docker compose for the stand project; stdin may be nil.
func (s *stand) compose(ctx context.Context, env []string, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", s.composeFile}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func pathEscape(p string) string { return url.PathEscape(p) }
