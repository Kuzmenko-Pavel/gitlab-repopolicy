// Package gitlab is the client of the GitLab REST API v4 and GraphQL used by
// the hook (architecture §13.3). Every call has a timeout and counts against
// the call budget of the receive; failures are classified, never hidden.
package gitlab

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrorKind classifies a failed call.
type ErrorKind int

// Error kinds.
const (
	// Unavailable: timeout, connection failure, 5xx, 429.
	Unavailable ErrorKind = iota
	// Unauthorized: 401 or 403, the token is expired, revoked or lacks a scope.
	Unauthorized
	// NotFound: 404, the object does not exist or is not visible to the token.
	NotFound
	// Unexpected: any other status or a malformed response.
	Unexpected
	// BudgetExceeded: the receive used up max_api_calls.
	BudgetExceeded
	// Deadline: the deadline of the hook invocation passed.
	Deadline
)

func (k ErrorKind) String() string {
	return [...]string{"unavailable", "unauthorized", "not_found", "unexpected", "budget_exceeded", "deadline"}[k]
}

// Error is a failed API call.
type Error struct {
	Kind   ErrorKind
	Status int
	Path   string
	Err    error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("gitlab %s: %s", e.Path, e.Kind)
	if e.Status != 0 {
		msg += fmt.Sprintf(" (HTTP %d)", e.Status)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf returns the kind of a call error; any error that is not *Error is Unexpected.
func KindOf(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return Unexpected
}

// Options configure a Client.
type Options struct {
	// Socket is the Workhorse unix socket; empty for https.
	Socket string
	// BaseURL is prepended to /api/v4 and /api/graphql.
	BaseURL string
	// CAFile is an extra CA bundle for https.
	CAFile string
	// TokenFile holds the service account token; read on first use and again after a 401.
	TokenFile string
	Timeout   time.Duration
	MaxCalls  int
	UserAgent string
}

// Client calls the GitLab API. It is not safe for concurrent use: the hook and
// every command call the API one request after another.
type Client struct {
	http      *http.Client
	base      string
	tokenFile string
	userAgent string
	timeout   time.Duration
	maxCalls  int

	token string
	calls int
}

// New creates a client; it does not touch the network or the token.
func New(o Options) (*Client, error) {
	transport := &http.Transport{
		MaxIdleConns:        4,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: o.Timeout,
	}
	if o.Socket != "" {
		dialer := &net.Dialer{}
		socket := o.Socket
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}
	} else {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if o.CAFile != "" {
			pem, err := os.ReadFile(o.CAFile) //nolint:gosec // G304: path comes from the server config
			if err != nil {
				return nil, fmt.Errorf("gitlab: ca_file: %w", err)
			}
			pool, err := x509.SystemCertPool()
			if err != nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("gitlab: ca_file: no certificates")
			}
			cfg.RootCAs = pool
		}
		transport.TLSClientConfig = cfg
	}
	if o.MaxCalls < 1 {
		return nil, errors.New("gitlab: MaxCalls must be positive")
	}
	return &Client{
		http:      &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		base:      strings.TrimSuffix(o.BaseURL, "/"),
		tokenFile: o.TokenFile,
		userAgent: o.UserAgent,
		timeout:   o.Timeout,
		maxCalls:  o.MaxCalls,
	}, nil
}

// Calls returns the number of API calls made so far.
func (c *Client) Calls() int { return c.calls }

func (c *Client) readToken(force bool) (string, bool, error) {
	if c.token != "" && !force {
		return c.token, false, nil
	}
	data, err := os.ReadFile(c.tokenFile) //nolint:gosec // G304: path comes from the server config
	if err != nil {
		return "", false, err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", false, errors.New("token file is empty")
	}
	changed := token != c.token
	c.token = token
	return token, changed, nil
}

func (c *Client) take() bool {
	if c.calls >= c.maxCalls {
		return false
	}
	c.calls++
	return true
}

// maxResponse bounds a response body.
const maxResponse = 4 << 20

// do performs one API request and decodes a 2xx JSON response into out.
// A 401 rereads the token file once: token rotate may have replaced it.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	token, _, err := c.readToken(false)
	if err != nil {
		return &Error{Kind: Unauthorized, Path: path, Err: fmt.Errorf("token: %w", err)}
	}
	status, err := c.once(ctx, method, path, token, body, out)
	if status == http.StatusUnauthorized {
		if fresh, changed, rerr := c.readToken(true); rerr == nil && changed {
			_, err = c.once(ctx, method, path, fresh, body, out)
		}
	}
	return err
}

func (c *Client) once(ctx context.Context, method, path, token string, body, out any) (int, error) {
	if ctx.Err() != nil {
		return 0, &Error{Kind: Deadline, Path: path, Err: ctx.Err()}
	}
	if !c.take() {
		return 0, &Error{Kind: BudgetExceeded, Path: path}
	}
	rctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, &Error{Kind: Unexpected, Path: path, Err: err}
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(rctx, method, c.base+path, rd)
	if err != nil {
		return 0, &Error{Kind: Unexpected, Path: path, Err: err}
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, &Error{Kind: Deadline, Path: path, Err: ctx.Err()}
		}
		return 0, &Error{Kind: Unavailable, Path: path, Err: err}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if cerr := resp.Body.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return resp.StatusCode, &Error{Kind: Unavailable, Status: resp.StatusCode, Path: path, Err: err}
	}
	switch s := resp.StatusCode; {
	case s >= 200 && s < 300:
	case s == http.StatusUnauthorized || s == http.StatusForbidden:
		return s, &Error{Kind: Unauthorized, Status: s, Path: path, Err: message(data)}
	case s == http.StatusNotFound:
		return s, &Error{Kind: NotFound, Status: s, Path: path}
	case s == http.StatusTooManyRequests || s >= 500:
		return s, &Error{Kind: Unavailable, Status: s, Path: path}
	default:
		return s, &Error{Kind: Unexpected, Status: s, Path: path, Err: message(data)}
	}
	if len(data) > maxResponse {
		return resp.StatusCode, &Error{Kind: Unexpected, Status: resp.StatusCode, Path: path, Err: errors.New("response too large")}
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
	} else if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, &Error{Kind: Unexpected, Status: resp.StatusCode, Path: path, Err: err}
		}
	}
	return resp.StatusCode, nil
}

// maxMessage bounds the error text taken from a response.
const maxMessage = 300

// message returns the "message" or "error" of a GitLab error response, nil without one.
func message(data []byte) error {
	var body struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil {
		return nil
	}
	text := body.Error
	if body.Message != nil {
		if s, ok := body.Message.(string); ok {
			text = s
		} else if b, err := json.Marshal(body.Message); err == nil {
			text = string(b)
		}
	}
	if text == "" {
		return nil
	}
	if len(text) > maxMessage {
		text = text[:maxMessage] + "..."
	}
	return errors.New(text)
}
