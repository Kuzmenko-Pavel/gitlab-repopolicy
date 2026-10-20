// Package audit defines audit events and writes them as JSON lines.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// MaxEventSize keeps every event line atomic for O_APPEND writers on local file systems.
const MaxEventSize = 4096

// Event names.
const (
	EventDecision                    = "decision"
	EventPolicyChange                = "policy_change"
	EventPolicyBootstrap             = "policy_bootstrap"
	EventPolicyRecovery              = "policy_recovery"
	EventInvalidPolicy               = "invalid_policy"
	EventInvalidCommitPolicy         = "invalid_commit_policy"
	EventInvalidBranchPolicy         = "invalid_branch_policy"
	EventAuthorizationBackendFailure = "authorization_backend_failure"
	EventUnknownPrincipal            = "unknown_principal"
	EventBreakglass                  = "breakglass"
	EventSettingsDrift               = "settings_drift"
	EventInternalError               = "internal_error"
)

// Event is one audit record. The writer fills Time, Enforced, Version, Git and Node.
type Event struct {
	Time  string `json:"ts"`
	Event string `json:"event"`
	// ReceiveID is the same in every event of one run of the hook.
	ReceiveID   string      `json:"receive_id,omitempty"`
	Module      string      `json:"module,omitempty"`
	Decision    string      `json:"decision,omitempty"`
	Enforced    bool        `json:"enforced"`
	Code        string      `json:"code,omitempty"`
	Action      string      `json:"action,omitempty"`
	ProjectID   int64       `json:"project_id,omitempty"`
	ProjectPath string      `json:"project_path,omitempty"`
	UserID      int64       `json:"user_id,omitempty"`
	Username    string      `json:"username,omitempty"`
	GLID        string      `json:"gl_id,omitempty"`
	Protocol    string      `json:"protocol,omitempty"`
	Ref         string      `json:"ref,omitempty"`
	OldRev      string      `json:"oldrev,omitempty"`
	NewRev      string      `json:"newrev,omitempty"`
	AnchorRev   string      `json:"anchor_rev,omitempty"`
	PolicyBlob  string      `json:"policy_blob,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Violations  []Violation `json:"violations,omitempty"`
	// Presets are the presets of the server config that the policy file of
	// the module extends.
	Presets     []Preset `json:"presets,omitempty"`
	Rules       []string `json:"rules,omitempty"`
	Signer      string   `json:"signer,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Truncated   bool     `json:"truncated,omitempty"`
	APICalls    int      `json:"api_calls"`
	DurationMS  int64    `json:"duration_ms"`
	Version     string   `json:"version"`
	Git         string   `json:"git,omitempty"`
	Node        string   `json:"node,omitempty"`
}

// Violation is the audit view of a module violation; Reason may hold API results.
type Violation struct {
	Rule    string   `json:"rule,omitempty"`
	Code    string   `json:"code,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Commits []string `json:"commits,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	// Source lists the layers the rules of the violation come from in merge
	// order: "preset:<name>" or "file".
	Source []string `json:"source,omitempty"`
}

// Preset is a preset of the server config with the SHA-256 of its canonical
// JSON, so the audit log tells which version of the preset applied.
type Preset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Writer serialises events to a file opened with O_APPEND and, optionally, to
// stdout. It is not safe for concurrent use; the hook and every command write
// their events one after another.
type Writer struct {
	path     string
	stdout   io.Writer
	enforced bool
	version  string
	git      string
	node     string
	clock    func() time.Time
}

// Options configure a Writer.
type Options struct {
	// File is the audit log; empty disables it.
	File string
	// Stdout receives a copy of each event when not nil.
	Stdout io.Writer
	// Enforced is false in mode: audit.
	Enforced bool
	Version  string
	Git      string
	Node     string
	Clock    func() time.Time
}

// NewWriter returns a writer; the file is opened on every write so that logrotate needs no copytruncate.
func NewWriter(o Options) *Writer {
	return &Writer{path: o.File, stdout: o.Stdout, enforced: o.Enforced, version: o.Version, git: o.Git, node: o.Node, clock: o.Clock}
}

// SetGit records the git version once it is known.
func (w *Writer) SetGit(v string) { w.git = v }

// Write appends one event as a single line.
func (w *Writer) Write(e Event) error {
	e.Time = w.clock().UTC().Format("2006-01-02T15:04:05.000Z")
	e.Enforced = w.enforced
	e.Version, e.Git, e.Node = w.version, w.git, w.node
	line, err := Encode(e)
	if err != nil {
		return err
	}
	if w.path != "" {
		if err := appendLine(w.path, line); err != nil {
			return err
		}
	}
	if w.stdout != nil {
		if _, err := w.stdout.Write(line); err != nil {
			return fmt.Errorf("audit stdout: %w", err)
		}
	}
	return nil
}

func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640) //nolint:gosec // G304: path comes from the server config
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("audit: %w", werr)
	}
	if cerr != nil {
		return fmt.Errorf("audit: %w", cerr)
	}
	return nil
}

// Encode returns the event as one JSON line of at most MaxEventSize bytes:
// violations and then the reason are cut until it fits, and Truncated is set.
func Encode(e Event) ([]byte, error) {
	for {
		data, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		if len(data)+1 <= MaxEventSize {
			return append(data, '\n'), nil
		}
		e.Truncated = true
		switch {
		case len(e.Violations) > 0 && trimItems(e.Violations):
		case len(e.Violations) > 0:
			e.Violations = e.Violations[:len(e.Violations)-1]
		case len(e.Reason) > 0:
			e.Reason = e.Reason[:len(e.Reason)/2]
		default:
			return nil, fmt.Errorf("audit: event of %d bytes cannot be truncated", len(data))
		}
	}
}

// trimItems halves the longest path or commit list; it reports false when no list is left to cut.
func trimItems(vs []Violation) bool {
	var longest *[]string
	for i := range vs {
		for _, l := range []*[]string{&vs[i].Paths, &vs[i].Commits} {
			if len(*l) > 1 && (longest == nil || len(*l) > len(*longest)) {
				longest = l
			}
		}
	}
	if longest == nil {
		return false
	}
	*longest = (*longest)[:len(*longest)/2]
	return true
}
