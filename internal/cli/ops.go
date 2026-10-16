package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/breakglass"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/buildinfo"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/install"
)

// DefaultBinary is where the package installs the binary.
const DefaultBinary = "/usr/bin/gitlab-repopolicy"

// fail prints an error of a command and returns code.
func fail(w io.Writer, cmd string, err error, code int) int {
	out := &output{w: w}
	out.printf("gitlab-repopolicy %s: %v\n", cmd, err)
	return withOutput(out.err, code)
}

// parse parses flags and rejects positional arguments.
func parse(fs *flag.FlagSet, args []string, stderr io.Writer) bool {
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return false
	}
	if fs.NArg() > 0 {
		out := &output{w: stderr}
		out.printf("gitlab-repopolicy %s: unexpected arguments %q\n", fs.Name(), fs.Args())
		return false
	}
	return true
}

// runInstallHook implements "install-hook --hooks-dir DIR [--user git]".
func (a App) runInstallHook(_ context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("install-hook", flag.ContinueOnError)
	hooksDir := fs.String("hooks-dir", "", "custom_hooks_dir of Gitaly")
	userName := fs.String("user", "git", "user that Gitaly runs hooks as")
	binary := fs.String("binary", DefaultBinary, "the gitlab-repopolicy binary the wrapper runs")
	configPath := fs.String("config", DefaultConfig, "server config the wrapper passes")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	if *hooksDir == "" {
		return fail(stdio.Err, "install-hook", fmt.Errorf("--hooks-dir is required"), ExitUsage)
	}
	sys := a.system()
	if !sys.IsRoot() {
		return fail(stdio.Err, "install-hook", errNotRoot, ExitError)
	}
	uid, gid, err := sys.LookupUser(*userName)
	if err != nil {
		return fail(stdio.Err, "install-hook", fmt.Errorf("--user: %w", err), ExitError)
	}
	path, res, err := install.Install(install.Options{HooksDir: *hooksDir, Binary: *binary, Config: *configPath,
		Chown: func(path string) error { return sys.Chown(path, uid, gid) }})
	if err != nil {
		return fail(stdio.Err, "install-hook", err, ExitError)
	}
	out := &output{w: stdio.Out}
	out.printf("%s: %s\n", path, res)
	lines, ok := a.ownFiles(*configPath, uid, gid)
	for _, line := range lines {
		out.printf("%s\n", line)
	}
	if !ok {
		out.printf("The hook cannot read its files: fix the owners above, then run gitlab-repopolicy selftest\n")
		return withOutput(out.err, ExitError)
	}
	out.printf("Next: gitlab-repopolicy selftest\n")
	return withOutput(out.err, ExitOK)
}

// ownFiles gives the hook user what it reads and writes: the group of
// config.yaml and of the token, the audit log directory and file. The log is
// narrowed to 0750 and 0640, because it holds usernames and paths. An owner or
// a mode that is already right is not touched. The package does the same at
// its first installation when the user exists.
func (a App) ownFiles(configPath string, uid, gid int) ([]string, bool) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return []string{fmt.Sprintf("%s: not read (%v)", configPath, err)}, false
	}
	type target struct {
		path     string
		uid, gid int
		// mode is the widest mode allowed, 0 to keep any.
		mode fs.FileMode
	}
	targets := []target{{configPath, -1, gid, 0}, {cfg.TokenFile, -1, gid, 0}}
	if cfg.Audit.File != "" {
		targets = append(targets, target{filepath.Dir(cfg.Audit.File), uid, gid, 0o750}, target{cfg.Audit.File, uid, gid, 0o640})
	}
	var lines []string
	ok := true
	for _, t := range targets {
		line, err := a.own(t.path, t.uid, t.gid, t.mode)
		if err != nil {
			ok = false
			line = fmt.Sprintf("%s: %v", t.path, err)
		}
		lines = append(lines, line)
	}
	return lines, ok
}

// own sets the owner (-1 keeps it) and narrows the mode of path and says what it did.
func (a App) own(path string, uid, gid int, mode fs.FileMode) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path + ": does not exist yet", nil
	}
	if err != nil {
		return "", err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("the owner cannot be read")
	}
	was := fmt.Sprintf("%d:%d", st.Uid, st.Gid)
	wantUID, wantGID := int(st.Uid), int(st.Gid)
	if uid >= 0 {
		wantUID = uid
	}
	if gid >= 0 {
		wantGID = gid
	}
	want := fmt.Sprintf("%d:%d", wantUID, wantGID)
	owner := "owner unchanged (" + was + ")"
	if want != was {
		if err := a.system().Chown(path, uid, gid); err != nil {
			return "", err
		}
		owner = "owner set to " + want + " (was " + was + ")"
	}
	if mode == 0 {
		return path + ": " + owner, nil
	}
	perm := info.Mode().Perm()
	if perm&^mode == 0 {
		return fmt.Sprintf("%s: %s, mode unchanged (%04o)", path, owner, perm), nil
	}
	if err := os.Chmod(path, perm&mode); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s: %s, mode set to %04o (was %04o)", path, owner, perm&mode, perm), nil
}

// runUninstallHook implements "uninstall-hook --hooks-dir DIR": only the wrapper is removed.
func (a App) runUninstallHook(_ context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("uninstall-hook", flag.ContinueOnError)
	hooksDir := fs.String("hooks-dir", "", "custom_hooks_dir of Gitaly")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	if *hooksDir == "" {
		return fail(stdio.Err, "uninstall-hook", fmt.Errorf("--hooks-dir is required"), ExitUsage)
	}
	if !a.system().IsRoot() {
		return fail(stdio.Err, "uninstall-hook", errNotRoot, ExitError)
	}
	path, res, err := install.Uninstall(*hooksDir)
	if err != nil {
		return fail(stdio.Err, "uninstall-hook", err, ExitError)
	}
	out := &output{w: stdio.Out}
	if res == install.Unchanged {
		out.printf("%s: not installed\n", path)
	} else {
		out.printf("%s: %s\n", path, res)
	}
	return withOutput(out.err, ExitOK)
}

// projectByPath looks a project up by its full path with the service account.
func projectByPath(ctx context.Context, cfg *config.Config, path string) (gitlab.Project, error) {
	api, err := apiClient(cfg, opsMaxCalls)
	if err != nil {
		return gitlab.Project{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	return api.Project(ctx, path)
}

// auditWriter writes the events of a command other than the hook: to the
// file and, with audit.stdout, to stdout.
func (a App) auditWriter(cfg *config.Config, stdout io.Writer) (*audit.Writer, error) {
	if err := a.prepareAuditFile(cfg.Audit.File); err != nil {
		return nil, err
	}
	o := audit.Options{File: cfg.Audit.File, Enforced: cfg.Mode == config.ModeEnforce, Version: buildinfo.Resolve().Version, Clock: time.Now}
	if cfg.Audit.Stdout {
		o.Stdout = stdout
	}
	if node, err := os.Hostname(); err == nil {
		o.Node = node
	}
	return audit.NewWriter(o), nil
}

// runBreakglass implements "breakglass grant|revoke|list".
func (a App) runBreakglass(ctx context.Context, args []string, env []string, stdio Stdio) int {
	if len(args) == 0 || (args[0] != "grant" && args[0] != "revoke" && args[0] != "list") {
		return fail(stdio.Err, "breakglass", fmt.Errorf("expected grant, revoke or list"), ExitUsage)
	}
	sub := args[0]
	fs := flag.NewFlagSet("breakglass "+sub, flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config")
	var projectID *int64
	var projectPath *string
	var ttl *time.Duration
	var reason *string
	if sub != "list" {
		projectID = fs.Int64("project-id", 0, "project ID, when GitLab does not answer")
		projectPath = fs.String("project", "", "full path of the project, looked up through the API")
	}
	if sub == "grant" {
		ttl = fs.Duration("ttl", 0, "how long the grant lasts, at most 24h")
		reason = fs.String("reason", "", "why, for example the incident")
	}
	if !parse(fs, args[1:], stdio.Err) {
		return ExitUsage
	}
	cmd := "breakglass " + sub
	if projectID != nil && (*projectID > 0) == (*projectPath != "") {
		return fail(stdio.Err, cmd, fmt.Errorf("give exactly one of --project and --project-id"), ExitUsage)
	}
	if !a.system().IsRoot() {
		return fail(stdio.Err, cmd, errNotRoot, ExitError)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return fail(stdio.Err, cmd, err, ExitError)
	}
	store := breakglass.Store{Dir: cfg.BreakglassDir, TrustedUID: a.system().TrustedUID()}
	out := &output{w: stdio.Out}
	now := time.Now()
	if sub == "list" {
		entries, err := store.List()
		if err != nil {
			return fail(stdio.Err, cmd, err, ExitError)
		}
		if len(entries) == 0 {
			out.printf("no break-glass grants\n")
		}
		for _, e := range entries {
			switch {
			case e.Err != nil:
				out.printf("%s: ignored: %v\n", e.Name, e.Err)
			case e.Grant.Valid(now):
				out.printf("%s: active: %s\n", e.Name, e.Grant.Text())
			default:
				out.printf("%s: expired: %s\n", e.Name, e.Grant.Text())
			}
		}
		return withOutput(out.err, ExitOK)
	}
	name := fmt.Sprintf("project %d", *projectID)
	if *projectPath != "" {
		p, err := projectByPath(ctx, cfg, *projectPath)
		if err != nil {
			return fail(stdio.Err, cmd, fmt.Errorf("--project %s: %w; when GitLab does not answer, give --project-id, the ID on the project overview page", *projectPath, err), ExitError)
		}
		*projectID, *projectPath = p.ID, p.PathWithNamespace
		name = fmt.Sprintf("project %d (%s)", p.ID, p.PathWithNamespace)
	}
	w, err := a.auditWriter(cfg, stdio.Out)
	if err != nil {
		return fail(stdio.Err, cmd, err, ExitError)
	}
	ev := audit.Event{Event: audit.EventBreakglass, Action: sub, ProjectID: *projectID, ProjectPath: *projectPath, Username: a.operator(env)}
	if sub == "grant" {
		g := breakglass.Grant{ProjectID: *projectID, GrantedAt: now.UTC(), ExpiresAt: now.UTC().Add(*ttl),
			Reason: *reason, Operator: ev.Username}
		if err := store.Grant(g); err != nil {
			return fail(stdio.Err, cmd, err, ExitError)
		}
		ev.Reason = g.Text()
		out.printf("%s: break-glass active until %s\n", name, g.ExpiresAt.Format(time.RFC3339))
	} else {
		had, err := store.Revoke(*projectID)
		if err != nil {
			return fail(stdio.Err, cmd, err, ExitError)
		}
		if !had {
			out.printf("%s: no break-glass grant\n", name)
			return withOutput(out.err, ExitOK)
		}
		out.printf("%s: break-glass revoked\n", name)
	}
	if err := w.Write(ev); err != nil {
		return fail(stdio.Err, cmd, err, ExitError)
	}
	return withOutput(out.err, ExitOK)
}
