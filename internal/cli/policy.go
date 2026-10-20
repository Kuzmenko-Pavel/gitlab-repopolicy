package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/branch"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/commit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/tag"
)

// maxRangeCommits bounds lint-commit --range.
const maxRangeCommits = 100_000

// moduleSettings are the module settings of validate and lint-commit: the
// defaults with the presets of an optional server config.
func moduleSettings(configPath string) (modules.Settings, error) {
	cfg := config.Defaults()
	if configPath != "" {
		data, err := readConfig(configPath)
		if err != nil {
			return modules.Settings{}, err
		}
		if cfg.Presets, err = config.Presets(data); err != nil {
			return modules.Settings{}, err
		}
	}
	return settingsOf(&cfg), nil
}

// readPolicyFile reads a policy file up to the size limit of its module.
func readPolicyFile(path string, m module.Module) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the file to check is a flag
	if err != nil {
		return nil, err
	}
	limit := m.MaxPolicySize()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && len(data) > limit {
		err = fmt.Errorf("%d bytes or more, the limit is %d", len(data), limit)
	}
	return data, err
}

// target is a policy file that validate checks.
type target struct {
	path string
	mod  module.Module
}

// runValidate implements "validate [-f FILE] [--module NAME] [--config FILE]
// [--online [--project P]] [--branch NAME] [--tag NAME]": the checks of
// acceptance; with --online also the principals through the API of the
// server config; --branch and --tag tell what the merged BRANCHPOLICY or
// TAGPOLICY requires of a name.
func (a App) runValidate(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	file := fs.String("f", "", "policy file; default: every policy file in the current directory")
	name := fs.String("module", "", "module of the file when its name is not a policy file name")
	configPath := fs.String("config", "", "server config: presets; with --online also gitlab_url and token_file")
	online := fs.Bool("online", false, "also check that principals exist, through the GitLab API of --config")
	project := fs.String("project", "", "with --online: project ID or path, to check the access of admins")
	branchName := fs.String("branch", "", "show the BRANCHPOLICY rule of a new branch with this name and its result")
	tagName := fs.String("tag", "", "show the TAGPOLICY rules of a tag with this name and their signers")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	out := &output{w: stdio.Out}
	usage := func(format string, a ...any) int {
		out.w = stdio.Err
		out.printf("gitlab-repopolicy validate: "+format+"\n", a...)
		return withOutput(out.err, ExitUsage)
	}
	if fs.NArg() > 0 {
		return usage("unexpected arguments %q", fs.Args())
	}
	if *online && *configPath == "" {
		return usage("--online needs --config with gitlab_url and token_file")
	}
	s, err := moduleSettings(*configPath)
	if err != nil {
		out.w = stdio.Err
		out.printf("gitlab-repopolicy validate: %v\n", err)
		return withOutput(out.err, ExitError)
	}
	mods := a.Modules(s).All()
	var v module.Validator
	if *online {
		ctx2, cancel := context.WithTimeout(ctx, queryTimeout)
		defer cancel()
		ctx = ctx2
		if v, err = a.onlineValidator(ctx, *configPath, *project); err != nil {
			out.w = stdio.Err
			out.printf("gitlab-repopolicy validate: %v\n", err)
			return withOutput(out.err, ExitError)
		}
	}
	// queries are the modules whose file --branch and --tag need.
	queries := map[string]string{}
	if *branchName != "" {
		queries["branchpolicy"] = "--branch"
	}
	if *tagName != "" {
		queries["tagpolicy"] = "--tag"
	}
	var targets []target
	switch {
	case *file != "":
		var m module.Module
		for _, c := range mods {
			if (*name != "" && c.Name() == *name) || (*name == "" && c.File() == filepath.Base(*file)) {
				m = c
			}
		}
		if m == nil {
			return usage("%s is not a policy file; use --module with one of %s", *file, moduleNames(mods))
		}
		targets = append(targets, target{*file, m})
	default:
		for _, m := range mods {
			if _, err := os.Stat(m.File()); err == nil {
				targets = append(targets, target{m.File(), m})
			}
		}
	}
	for _, mod := range slices.Sorted(maps.Keys(queries)) {
		flagName := queries[mod]
		if !slices.ContainsFunc(targets, func(t target) bool { return t.mod.Name() == mod }) {
			return usage("%s needs a %s file: -f FILE or %s in the current directory", flagName, strings.ToUpper(mod), strings.ToUpper(mod))
		}
	}
	// A broken preset does not stop the hook: only the files that extend it
	// suffer, so validate reports it on its own.
	code := ExitOK
	var presetErrs []string
	for _, m := range mods {
		if r, ok := m.(module.PresetReporter); ok {
			presetErrs = append(presetErrs, r.PresetErrors()...)
		}
	}
	if len(presetErrs) > 0 {
		out.printf("%s: invalid presets\n", *configPath)
		for _, e := range presetErrs {
			out.printf("  error: %s\n", e)
		}
		code = ExitError
	}
	if len(targets) == 0 {
		out.printf("no policy files in the current directory\n")
		return withOutput(out.err, code)
	}
	for _, tg := range targets {
		p, problems, err := validateFile(ctx, tg.path, tg.mod, v)
		if err != nil {
			out.printf("%s: invalid\n  %v\n", tg.path, err)
			code = ExitError
			continue
		}
		var errs, warns, notes []string
		for _, p := range problems {
			switch {
			case p.Unknown:
				notes = append(notes, p.Msg)
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
		for _, n := range notes {
			out.printf("  not checked: %s\n", n)
		}
		if len(errs) > 0 {
			continue
		}
		printOrigins(out, p)
		switch {
		case tg.mod.Name() == "branchpolicy" && *branchName != "":
			if bp, ok := p.(*branch.Policy); ok {
				printBranch(ctx, out, bp, *branchName, v)
			}
		case tg.mod.Name() == "tagpolicy" && *tagName != "":
			printTag(out, p, *tagName)
		}
	}
	return withOutput(out.err, code)
}

func validateFile(ctx context.Context, path string, m module.Module, v module.Validator) (module.Policy, []module.Problem, error) {
	data, err := readPolicyFile(path, m)
	if err != nil {
		return nil, nil, err
	}
	p, err := m.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	problems := p.Validate(ctx, v)
	if vv, ok := p.(module.VersionValidator); ok {
		problems = append(problems, vv.ValidateVersion(nil, time.Now())...)
	}
	return p, problems, nil
}

// printOrigins shows where the merged rules and their fields come from, for
// a file that extends presets.
func printOrigins(out *output, p module.Policy) {
	pu, ok := p.(module.PresetUser)
	if !ok || len(pu.Presets()) == 0 {
		return
	}
	names := make([]string, 0, len(pu.Presets()))
	for _, ref := range pu.Presets() {
		names = append(names, ref.Name)
	}
	out.printf("  extends: %s\n", strings.Join(names, ", "))
	for _, r := range pu.Origins() {
		out.printf("  rule %s: %s\n", r.Rule, strings.Join(r.Layers, ", "))
		for _, f := range r.Fields {
			out.printf("    %s: %s\n", f.Field, strings.Join(f.Layers, ", "))
		}
	}
}

// printBranch tells what the rule of a new branch name requires; the user
// prefix is checked only with --online.
func printBranch(ctx context.Context, out *output, p *branch.Policy, name string, v module.Validator) {
	r, ok := p.RuleFor(name)
	if !ok {
		out.printf("branch %s: no rule matches, the name is not checked\n", name)
		return
	}
	out.printf("branch %s: rule %s (match: %s; from %s)\n", name, r.Rule, r.Match, strings.Join(r.Source, ", "))
	denied := func(why string) {
		out.printf("  result: denied, %s\n", why)
		if r.Message != "" {
			out.printf("  message: %s\n", r.Message)
		}
	}
	if r.Deny {
		denied("branches of this class may not be created")
		return
	}
	var requires []string
	if r.UserPrefix {
		prefix, _, found := strings.Cut(name, "/")
		if !found || prefix == "" {
			denied("the name does not start with a GitLab username and /")
			return
		}
		if v == nil {
			requires = append(requires, prefix+" is an active GitLab user (checked with --online)")
		} else {
			users, st := v.Users(ctx, []string{prefix})
			u, found := users[strings.ToLower(prefix)]
			switch {
			case !st.IsConfirmed():
				requires = append(requires, prefix+" is an active GitLab user (not checked: "+st.Reason()+")")
			case !found || !u.Active:
				denied(prefix + " is not an active GitLab user")
				return
			default:
				out.printf("  %s is an active GitLab user\n", prefix)
			}
		}
	}
	if r.Protected {
		requires = append(requires, "the project protects the branch in Settings > Repository > Protected branches")
	}
	for _, req := range requires {
		out.printf("  requires: %s\n", req)
	}
	if len(requires) == 0 {
		out.printf("  result: allowed\n")
		return
	}
	out.printf("  result: allowed when the requirements hold\n")
}

// printTag tells which rules apply to a tag name and which keys may sign it;
// signatures are not checked offline.
func printTag(out *output, p module.Policy, name string) {
	r, ok := tag.RulesFor(p, name)
	if !ok {
		out.printf("tag %s: no rule matches, the tag is not checked\n", name)
		return
	}
	out.printf("tag %s: rules %s (from %s)\n", name, strings.Join(r.Rules, ", "), strings.Join(r.Source, ", "))
	if len(r.Signers) == 0 {
		out.printf("  signers: none, the matching rules have no signer in common; every such tag is denied\n")
	} else {
		out.printf("  signers: %s\n", strings.Join(r.Signers, ", "))
	}
	if r.Protected {
		out.printf("  requires: the project protects the tag in Settings > Repository > Protected tags\n")
	}
	out.printf("  the tag must be signed by one of the signers; validate does not check signatures\n")
}

// onlineValidator answers the online checks of validate --online. Without a
// project the access of admins cannot be checked and is reported as such.
func (a App) onlineValidator(ctx context.Context, configPath, project string) (module.Validator, error) {
	cfg, err := a.loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	api, err := apiClient(cfg, opsMaxCalls)
	if err != nil {
		return nil, err
	}
	if project == "" {
		return noProject{identity.NewAuthorizer(api, 0, nil, nil)}, nil
	}
	p, err := api.Project(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("--project %s: %w", project, err)
	}
	return identity.NewAuthorizer(api, p.ID, nil, nil), nil
}

// noProject is a validator without a project: access levels are unknown.
type noProject struct{ *identity.Authorizer }

func (noProject) AccessLevels(context.Context, []int64) (map[int64]int, module.Status) {
	return nil, module.Unknown("--project is not given")
}

func moduleNames(mods []module.Module) string {
	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Name())
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// runLintCommit implements "lint-commit [-m MSG | --range A..B [--skip-author EMAIL]...]
// [-f COMMITPOLICY] [--rule NAME]...": the server implementation applied locally. There is no
// branch, so every rule applies unless --rule names the ones of a branch.
func (a App) runLintCommit(ctx context.Context, args []string, env []string, stdio Stdio) int {
	fs := flag.NewFlagSet("lint-commit", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	message := fs.String("m", "", "commit message to check")
	revRange := fs.String("range", "", "commits to check: A..B or a single revision for its whole history")
	file := fs.String("f", "COMMITPOLICY", "COMMITPOLICY file")
	configPath := fs.String("config", "", "server config with presets; other keys are not read")
	var rules, skipAuthors names
	fs.Var(&rules, "rule", "check only this rule; repeat for several")
	fs.Var(&skipAuthors, "skip-author", "with --range: skip commits of this author email; repeat for several")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	errOut := &output{w: stdio.Err}
	if fs.NArg() > 0 || (*message == "") == (*revRange == "") {
		errOut.printf("gitlab-repopolicy lint-commit: give exactly one of -m and --range\n")
		return withOutput(errOut.err, ExitUsage)
	}
	if len(skipAuthors) > 0 && *revRange == "" {
		errOut.printf("gitlab-repopolicy lint-commit: --skip-author needs --range\n")
		return withOutput(errOut.err, ExitUsage)
	}
	s, err := moduleSettings(*configPath)
	if err != nil {
		errOut.printf("gitlab-repopolicy lint-commit: %v\n", err)
		return withOutput(errOut.err, ExitError)
	}
	settings := commit.NewSettings(s.Presets["commitpolicy"])
	data, err := readPolicyFile(*file, commit.New(settings))
	var linter *commit.Policy
	if err == nil {
		linter, err = commit.Parse(data, settings)
	}
	if err == nil {
		err = linter.Unusable()
	}
	if err == nil && len(rules) > 0 {
		linter, err = linter.Select(rules)
	}
	if err != nil {
		errOut.printf("gitlab-repopolicy lint-commit: %s: %v\n", *file, err)
		return withOutput(errOut.err, ExitError)
	}
	out := &output{w: stdio.Out}
	failed := false
	print := func(label string, findings []commit.LintFinding) {
		for _, f := range findings {
			lv := "warning"
			if f.Error {
				lv, failed = "error", true
			}
			out.printf("%s: [%s] %s: %s\n", label, f.Rule, lv, f.Reason)
		}
	}
	if *message != "" {
		print("message", linter.LintMessage(ctx, *message))
	} else if err := lintRange(ctx, env, *revRange, skipAuthors, linter, print, func(label, email string) {
		out.printf("%s: skipped, author %s\n", label, email)
	}); err != nil {
		errOut.printf("gitlab-repopolicy lint-commit: %v\n", err)
		return withOutput(errOut.err, ExitError)
	}
	if failed {
		return withOutput(out.err, ExitError)
	}
	return withOutput(out.err, ExitOK)
}

// names is a repeatable string flag.
type names []string

func (n *names) String() string { return strings.Join(*n, ",") }

func (n *names) Set(s string) error {
	*n = append(*n, s)
	return nil
}

func lintRange(ctx context.Context, env []string, revRange string, skipAuthors []string, linter *commit.Policy,
	print func(string, []commit.LintFinding), skipped func(string, string)) (err error) {
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
		label := c[:min(len(c), 9)]
		if email := authorEmail(data); slices.ContainsFunc(skipAuthors, func(s string) bool { return strings.EqualFold(s, email) }) {
			skipped(label, email)
			continue
		}
		print(label, linter.LintCommit(ctx, data))
	}
	return nil
}

// authorEmail is the email of the author line of a raw commit object.
func authorEmail(object []byte) string {
	header, _, _ := bytes.Cut(object, []byte("\n\n"))
	for _, line := range bytes.Split(header, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("author ")); ok {
			if i, j := bytes.IndexByte(rest, '<'), bytes.IndexByte(rest, '>'); i >= 0 && j > i {
				return string(rest[i+1 : j])
			}
			return ""
		}
	}
	return ""
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v
		}
	}
	return ""
}
