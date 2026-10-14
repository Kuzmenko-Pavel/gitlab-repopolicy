package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

// maxRangeCommits bounds lint-commit --range.
const maxRangeCommits = 100_000

// moduleSet builds the modules with the presets of an optional server config.
func (a App) moduleSet(configPath string) ([]module.Module, error) {
	s := modules.Settings{PatternTimeout: 100 * time.Millisecond, PatternBudget: 2 * time.Second, Clock: time.Now}
	if configPath != "" {
		cfg, err := loadConfig(configPath)
		if err != nil {
			return nil, err
		}
		s.CommitPresets = cfg.CommitPolicyPresets
		s.PatternTimeout, s.PatternBudget = cfg.Timeouts.Pattern.Std(), cfg.Timeouts.PatternBudget.Std()
	}
	return a.Modules(s), nil
}

// readPolicyFile reads a policy file up to the size limit of its module.
func readPolicyFile(path string, m module.Module) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the file to check is a flag
	if err != nil {
		return nil, err
	}
	limit := module.MaxPolicySize(m)
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && len(data) > limit {
		err = fmt.Errorf("%d bytes or more, the limit is %d", len(data), limit)
	}
	return data, err
}

// runValidate implements "validate [-f FILE] [--module NAME] [--config FILE]":
// the checks of acceptance without GitLab (architecture §18.7, D23).
func (a App) runValidate(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	file := fs.String("f", "", "policy file; default: every policy file in the current directory")
	name := fs.String("module", "", "module of the file when its name is not a policy file name")
	configPath := fs.String("config", "", "server config with commitpolicy_presets")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	out := &output{w: stdio.Out}
	if fs.NArg() > 0 {
		out.w = stdio.Err
		out.printf("gitlab-repopolicy validate: unexpected arguments %q\n", fs.Args())
		return withOutput(out.err, ExitUsage)
	}
	mods, err := a.moduleSet(*configPath)
	if err != nil {
		out.w = stdio.Err
		out.printf("gitlab-repopolicy validate: %v\n", err)
		return withOutput(out.err, ExitError)
	}
	var targets []struct {
		path string
		mod  module.Module
	}
	switch {
	case *file != "":
		m := findModule(mods, filepath.Base(*file), *name)
		if m == nil {
			out.w = stdio.Err
			out.printf("gitlab-repopolicy validate: %s is not a policy file; use --module with one of %s\n", *file, moduleNames(mods))
			return withOutput(out.err, ExitUsage)
		}
		targets = append(targets, struct {
			path string
			mod  module.Module
		}{*file, m})
	default:
		for _, m := range mods {
			if _, err := os.Stat(m.File()); err == nil {
				targets = append(targets, struct {
					path string
					mod  module.Module
				}{m.File(), m})
			}
		}
		if len(targets) == 0 {
			out.printf("no policy files in the current directory\n")
			return withOutput(out.err, ExitOK)
		}
	}
	code := ExitOK
	for _, tg := range targets {
		problems, err := validateFile(ctx, tg.path, tg.mod)
		if err != nil {
			out.printf("%s: invalid\n  %v\n", tg.path, err)
			code = ExitError
			continue
		}
		var errs, warns []string
		for _, p := range problems {
			switch {
			case p.Warning:
				warns = append(warns, p.Msg)
			default:
				errs = append(errs, p.Msg)
			}
		}
		status := "valid"
		if len(errs) > 0 {
			status, code = "invalid", ExitError
		}
		out.printf("%s: %s\n", tg.path, status)
		for _, e := range errs {
			out.printf("  error: %s\n", e)
		}
		for _, w := range warns {
			out.printf("  warning: %s\n", w)
		}
	}
	return withOutput(out.err, code)
}

func validateFile(ctx context.Context, path string, m module.Module) ([]module.Problem, error) {
	data, err := readPolicyFile(path, m)
	if err != nil {
		return nil, err
	}
	p, err := m.Parse(data)
	if err != nil {
		return nil, err
	}
	found := p.Validate(ctx, nil)
	if vv, ok := p.(module.VersionValidator); ok {
		found = append(found, vv.ValidateVersion(nil, time.Now())...)
	}
	var problems []module.Problem
	for _, pr := range found {
		if pr.Unknown {
			continue
		}
		problems = append(problems, pr)
	}
	return problems, nil
}

func findModule(mods []module.Module, base, name string) module.Module {
	for _, m := range mods {
		if (name != "" && m.Name() == name) || (name == "" && m.File() == base) {
			return m
		}
	}
	return nil
}

func moduleNames(mods []module.Module) string {
	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Name())
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// runLintCommit implements "lint-commit [-m MSG | --range A..B] [-f COMMITPOLICY]":
// the server implementation applied locally; every ruleset applies (question 11).
func (a App) runLintCommit(ctx context.Context, args []string, env []string, stdio Stdio) int {
	fs := flag.NewFlagSet("lint-commit", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	message := fs.String("m", "", "commit message to check")
	revRange := fs.String("range", "", "commits to check: A..B or a single revision for its whole history")
	file := fs.String("f", "COMMITPOLICY", "COMMITPOLICY file")
	configPath := fs.String("config", "", "server config with commitpolicy_presets")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	errOut := &output{w: stdio.Err}
	if fs.NArg() > 0 || (*message == "") == (*revRange == "") {
		errOut.printf("gitlab-repopolicy lint-commit: give exactly one of -m and --range\n")
		return withOutput(errOut.err, ExitUsage)
	}
	mods, err := a.moduleSet(*configPath)
	if err != nil {
		errOut.printf("gitlab-repopolicy lint-commit: %v\n", err)
		return withOutput(errOut.err, ExitError)
	}
	m := findModule(mods, "COMMITPOLICY", "")
	if m == nil {
		errOut.printf("gitlab-repopolicy lint-commit: the COMMITPOLICY module is not available\n")
		return withOutput(errOut.err, ExitError)
	}
	data, err := readPolicyFile(*file, m)
	var linter module.CommitLinter
	if err == nil {
		var p module.Policy
		if p, err = m.Parse(data); err == nil {
			var ok bool
			if linter, ok = p.(module.CommitLinter); !ok {
				err = errors.New("the policy cannot lint commits")
			}
		}
	}
	if err != nil {
		errOut.printf("gitlab-repopolicy lint-commit: %s: %v\n", *file, err)
		return withOutput(errOut.err, ExitError)
	}
	out := &output{w: stdio.Out}
	failed := false
	print := func(label string, findings []module.LintFinding) {
		for _, f := range findings {
			lv := "warning"
			if f.Error {
				lv, failed = "error", true
			}
			out.printf("%s: [%s] %s: %s\n", label, f.Ruleset, lv, f.Reason)
		}
	}
	if *message != "" {
		print("message", linter.LintMessage(ctx, *message))
	} else if err := lintRange(ctx, env, *revRange, linter, print); err != nil {
		errOut.printf("gitlab-repopolicy lint-commit: %v\n", err)
		return withOutput(errOut.err, ExitError)
	}
	if failed {
		return withOutput(out.err, ExitError)
	}
	return withOutput(out.err, ExitOK)
}

func lintRange(ctx context.Context, env []string, revRange string, linter module.CommitLinter, print func(string, []module.LintFinding)) (err error) {
	path := envValue(env, "PATH")
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir, err := gitx.Discover(ctx, path, cwd)
	if err != nil {
		return err
	}
	repo, err := gitx.Open(gitx.Env{Dir: dir, Path: path})
	if err != nil {
		return err
	}
	defer func() {
		if cerr := repo.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	o := gitx.RevListOptions{Max: maxRangeCommits}
	from, to, isRange := strings.Cut(revRange, "..")
	if !isRange {
		to, from = from, ""
	}
	tip, err := repo.ResolveCommit(ctx, to)
	if err != nil {
		return err
	}
	o.Include = []string{tip}
	if from != "" {
		base, err := repo.ResolveCommit(ctx, from)
		if err != nil {
			return err
		}
		o.Exclude = []string{base}
	}
	commits, err := repo.RevList(ctx, o)
	if err != nil {
		return err
	}
	slices.Reverse(commits)
	for _, c := range commits {
		_, data, err := repo.Read(ctx, c, 1<<20)
		if err != nil {
			return err
		}
		print(c[:min(len(c), 9)], linter.LintCommit(ctx, data))
	}
	return nil
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v
		}
	}
	return ""
}
