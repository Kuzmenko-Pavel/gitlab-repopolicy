// Package cli implements the gitlab-repopolicy command line and acts as the composition root.
package cli

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/buildinfo"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

// Exit codes returned by Run.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Stdio carries the standard streams of the process.
type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Build describes the binary being run.
type Build struct {
	Version string
	Commit  string
	Date    string
	Go      string
}

type command struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string, env []string, stdio Stdio) int
}

// App is the composition root. Modules builds the module list from the
// server config; tests replace it to run the core with test modules.
type App struct {
	Modules func(modules.Settings) []module.Module
	// System is the operating system as root commands see it; nil is the real one.
	System System
}

// Run executes the command line args (without the program name) with the default modules.
func Run(ctx context.Context, args []string, env []string, stdio Stdio) int {
	return App{Modules: modules.Default}.Run(ctx, args, env, stdio)
}

// Run executes the command line args (without the program name) and returns the exit code.
func (a App) Run(ctx context.Context, args []string, env []string, stdio Stdio) int {
	commands := a.commandList()
	if len(args) == 0 {
		return withOutput(usage(stdio.Err, commands), ExitUsage)
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		return withOutput(usage(stdio.Out, commands), ExitOK)
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(ctx, args[1:], env, stdio)
		}
	}
	out := &output{w: stdio.Err}
	out.printf("gitlab-repopolicy: unknown command %q\n\n", args[0])
	if out.err != nil {
		return ExitError
	}
	return withOutput(usage(stdio.Err, commands), ExitUsage)
}

func (a App) commandList() []command {
	return []command{
		{name: "hook", summary: "run as the pre-receive hook of Gitaly: hook pre-receive --config FILE", run: a.runHook},
		{name: "install-hook", summary: "connect the hook to Gitaly: --hooks-dir DIR [--user git] (root)", run: a.runInstallHook},
		{name: "uninstall-hook", summary: "remove the hook wrapper: --hooks-dir DIR (root)", run: a.runUninstallHook},
		{name: "selftest", summary: "check config, token, service account, API and audit log [--config FILE]", run: a.runSelftest},
		{name: "settings-check", summary: "compare settings of projects with PATHPOLICY against the server config", run: a.runSettingsCheck},
		{name: "coverage", summary: "find top-level groups without the service account: --token-file ADMIN_TOKEN", run: a.runCoverage},
		{name: "token", summary: "rotate the service account token: token rotate [--before 30d] (root)", run: a.runToken},
		{name: "breakglass", summary: "grant | revoke | list break-glass for a project (root)", run: a.runBreakglass},
		{name: "explain", summary: "which path rules match and whether they allow a user: --project P --user U --path X [--ref R]", run: a.runExplain},
		{name: "admins", summary: "configured, effective and missing admins of PATHPOLICY: --project P", run: a.runAdmins},
		{name: "test", summary: "the decision of the hook without a push: --repo DIR --old A --new B --ref R --user U", run: a.runTest},
		{name: "lint-commit", summary: "check commit messages against COMMITPOLICY: -m MSG | --range A..B [-f FILE]", run: a.runLintCommit},
		{name: "validate", summary: "check policy files as the hook does when it accepts them: [-f FILE] [--online]", run: a.runValidate},
		{name: "version", summary: "print version, commit, build date and Go version", run: runVersion},
	}
}

func usage(w io.Writer, commands []command) error {
	out := &output{w: w}
	out.printf("Usage: gitlab-repopolicy <command> [flags]\n\nCommands:\n")
	for _, c := range commands {
		out.printf("  %-20s %s\n", c.name, c.summary)
	}
	return out.err
}

// CurrentBuild returns the metadata of the running binary.
func CurrentBuild() Build {
	info := buildinfo.Resolve()
	return Build{
		Version: info.Version,
		Commit:  info.Commit,
		Date:    info.Date,
		Go:      runtime.Version(),
	}
}

// output writes formatted text and keeps the first write error.
type output struct {
	w   io.Writer
	err error
}

func (o *output) printf(format string, a ...any) {
	if o.err != nil {
		return
	}
	_, o.err = fmt.Fprintf(o.w, format, a...)
}

// withOutput returns code, or ExitError if writing the command output failed.
func withOutput(err error, code int) int {
	if err != nil {
		return ExitError
	}
	return code
}
