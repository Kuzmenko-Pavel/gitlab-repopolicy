// Package gitlabtest is a scripted fake of the GitLab API for tests. It listens
// on a unix socket or on TLS with a test CA, like the real endpoints of the hook.
package gitlabtest

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Response is one scripted answer.
type Response struct {
	Status int
	// Body is encoded as JSON unless it is a string.
	Body any
	// Delay holds the response back, for timeouts.
	Delay time.Duration
	// Drop closes the connection without a response.
	Drop bool
}

// OK is a 200 response with body.
func OK(body any) Response { return Response{Status: http.StatusOK, Body: body} }

// Status is an empty response with the status code.
func Status(code int) Response { return Response{Status: code} }

// Server is a fake GitLab API.
type Server struct {
	t      testing.TB
	srv    *httptest.Server
	socket string

	mu      sync.Mutex
	scripts map[string][]Response
	calls   map[string]int
	tokens  []string
	bodies  map[string][]string
}

// NewUnix starts a server on a unix socket in a temporary directory.
func NewUnix(t testing.TB) *Server {
	s := newServer(t)
	dir, err := os.MkdirTemp("", "gitlabtest")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	s.socket = filepath.Join(dir, "socket")
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", s.socket)
	if err != nil {
		t.Fatal(err)
	}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	if err := s.srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	s.srv.Listener = l
	s.srv.Start()
	t.Cleanup(s.srv.Close)
	return s
}

// NewTLS starts an HTTPS server with a test certificate.
func NewTLS(t testing.TB) *Server {
	s := newServer(t)
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func newServer(t testing.TB) *Server {
	return &Server{t: t, scripts: map[string][]Response{}, calls: map[string]int{}, bodies: map[string][]string{}}
}

// URL is the gitlab_url of the server: http+unix://<escaped socket> or https://host:port.
func (s *Server) URL() string {
	if s.socket != "" {
		return "http+unix://" + url.PathEscape(s.socket)
	}
	return s.srv.URL
}

// Socket returns the unix socket path, empty for TLS.
func (s *Server) Socket() string { return s.socket }

// BaseURL is the URL that API paths are appended to.
func (s *Server) BaseURL() string {
	if s.socket != "" {
		return "http://gitlab"
	}
	return s.srv.URL
}

// WriteCA writes the certificate of a TLS server to dir and returns the path.
func (s *Server) WriteCA(dir string) string {
	cert := s.srv.Certificate()
	path := filepath.Join(dir, "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path
}

// CertPool returns a pool with the certificate of a TLS server.
func (s *Server) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.srv.Certificate())
	return pool
}

// Handle scripts the responses of "METHOD path", where path includes the query.
// Responses are used in order; the last one repeats.
func (s *Server) Handle(method, path string, responses ...Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts[method+" "+path] = responses
}

// Calls returns how many times "METHOD path" was requested.
func (s *Server) Calls(method, path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[method+" "+path]
}

// TotalCalls returns the number of requests of all paths.
func (s *Server) TotalCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		n += c
	}
	return n
}

// Tokens returns the PRIVATE-TOKEN of every request, in order.
func (s *Server) Tokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tokens...)
}

// Bodies returns the request bodies of "METHOD path", in order.
func (s *Server) Bodies(method, path string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies[method+" "+path]...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.RequestURI()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		s.t.Log(err)
	}
	s.mu.Lock()
	s.calls[key]++
	if len(body) > 0 {
		s.bodies[key] = append(s.bodies[key], string(body))
	}
	s.tokens = append(s.tokens, r.Header.Get("PRIVATE-TOKEN"))
	script, ok := s.scripts[key]
	var resp Response
	if ok {
		resp = script[0]
		if len(script) > 1 {
			s.scripts[key] = script[1:]
		}
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, `{"message":"404 Not scripted: `+strings.ReplaceAll(key, `"`, "")+`"}`, http.StatusNotFound)
		return
	}
	if resp.Delay > 0 {
		select {
		case <-time.After(resp.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if resp.Drop {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				if err := conn.Close(); err != nil {
					s.t.Log(err)
				}
			}
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.Status)
	switch b := resp.Body.(type) {
	case nil:
	case string:
		_, _ = w.Write([]byte(b))
	default:
		if err := json.NewEncoder(w).Encode(b); err != nil {
			s.t.Error(err)
		}
	}
}
