package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/breakglass"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/buildinfo"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/core"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/hookenv"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/report"
)

// DefaultConfig is the server config of a package installation.
const DefaultConfig = "/etc/gitlab-repopolicy/config.yaml"

// runHook implements "hook pre-receive". In this mode stdout and stderr belong
// to report only: they reach the pusher. Exit 0 accepts the push.
func (a App) runHook(ctx context.Context, args []string, env []string, stdio Stdio) int {
	if len(args) == 0 || args[0] != "pre-receive" {
		out := &output{w: stdio.Err}
		out.printf("gitlab-repopolicy hook: expected %q\n", "pre-receive")
		return withOutput(out.err, ExitUsage)
	}
	fs := flag.NewFlagSet("hook pre-receive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", DefaultConfig, "server config")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return reject(stdio, "the hook is installed with invalid arguments")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil || cfg.Audit.File == "" {
		// The hook's stdout reaches the pusher, so audit.stdout does not apply
		// here and the hook needs audit.file.
		return reject(stdio, "the hook configuration on the server is invalid")
	}
	if cfg.Mode == config.ModeOff {
		return ExitOK
	}
	hookEnv, err := hookenv.ParseEnv(env)
	if err != nil {
		return reject(stdio, "the hook runs outside of Gitaly or with an incomplete environment")
	}
	if !hookEnv.IsProject() {
		return ExitOK
	}
	updates, err := hookenv.ParseUpdates(stdio.In)
	if err != nil {
		return reject(stdio, "the ref updates passed to the hook cannot be read")
	}
	git, err := gitx.Open(gitx.Env{Dir: hookEnv.Git.Dir, ObjectDirectory: hookEnv.Git.ObjectDirectory,
		AlternateObjectDirs: hookEnv.Git.AlternateObjectDirs, Path: hookEnv.Git.Path})
	if err != nil {
		return reject(stdio, "git is not available to the hook")
	}
	api, err := apiClient(cfg, cfg.MaxAPICalls)
	if err != nil {
		return reject(stdio, "the hook configuration on the server is invalid")
	}
	o := audit.Options{File: cfg.Audit.File, Enforced: cfg.Mode == config.ModeEnforce, Version: buildinfo.Resolve().Version, Clock: time.Now}
	if node, err := os.Hostname(); err == nil {
		o.Node = node
	}
	writer := audit.NewWriter(o)
	mods := a.enabledModules(cfg)
	auditor := &gitAudit{w: writer, git: git, ctx: ctx}
	out := core.Run(ctx, core.Settings{Mode: cfg.Mode, Total: cfg.Timeouts.Total.Std(), Help: cfg.Help}, core.Deps{
		Root: mods.Root, Modules: mods.Modules, Git: git, API: api, Audit: auditor, Clock: time.Now,
		Breakglass: breakglass.Store{Dir: cfg.BreakglassDir, TrustedUID: a.system().TrustedUID()},
	}, core.Request{Env: hookEnv, Updates: updates})
	if err := git.Close(); err != nil {
		// The decision is made; the only channel left for a failure is audit.
		if werr := auditor.Write(audit.Event{Event: audit.EventInternalError, ReceiveID: out.ReceiveID, Reason: "git cat-file: " + err.Error(),
			ProjectID: hookEnv.ProjectID, ProjectPath: hookEnv.ProjectPath}); werr != nil {
			out.AuditErr = errors.Join(out.AuditErr, werr)
		}
	}
	write(stdio.Out, out.Stdout)
	write(stdio.Err, out.Stderr)
	if out.AuditErr != nil {
		// The decision stands; the pusher is the first to see the broken log.
		write(stdio.Err, "gitlab-repopolicy: the audit log is not writable, tell your GitLab administrators\n")
	}
	if out.Allow {
		return ExitOK
	}
	return ExitError
}

// write prints hook output. A closed stream cannot change the decision, so a
// write error only stops the output.
func write(w io.Writer, s string) {
	if s == "" {
		return
	}
	if _, err := io.WriteString(w, s); err != nil {
		return
	}
}

// loadConfig reads and parses the server config.
func loadConfig(path string) (*config.Config, error) {
	data, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	return config.Parse(data)
}

// readConfig reads a config file up to one byte over the size limit, which
// the parser then rejects.
func readConfig(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the config path is a flag of the hook wrapper
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, config.MaxSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return data, err
}

// reject denies the push before the core runs: without a working config the
// hook cannot prove that the push is safe.
func reject(stdio Stdio, why string) int {
	write(stdio.Err, report.Rejection([]report.Section{{Name: report.CoreSection, Violations: []module.Violation{{
		Code: module.CodeInternalError, Facts: []string{why + ": this is a problem of the server, not of your change"},
		Summary: "internal error: " + why + "; the push is rejected, ask your GitLab administrators",
	}}}}, report.Context{}))
	return ExitError
}

// gitAudit adds the version of the hook's git to audit events; it is read on the first event.
type gitAudit struct {
	w      *audit.Writer
	git    *gitx.Repo
	ctx    context.Context
	loaded bool
}

func (g *gitAudit) Write(e audit.Event) error {
	if !g.loaded {
		g.loaded = true
		v, err := g.git.Version(g.ctx)
		if err != nil {
			v = "unknown: " + err.Error()
		}
		g.w.SetGit(v)
	}
	return g.w.Write(e)
}
