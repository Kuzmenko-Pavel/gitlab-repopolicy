package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/breakglass"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/core"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitx"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/hookenv"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/identity"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules/path"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/refs"
)

// queryTimeout bounds explain, admins and validate --online.
const queryTimeout = time.Minute

// settingsOf are the module settings of the server config.
func settingsOf(cfg *config.Config) modules.Settings {
	return modules.Settings{CommitPresets: cfg.CommitPolicyPresets,
		PatternTimeout: cfg.Timeouts.Pattern.Std(), PatternBudget: cfg.Timeouts.PatternBudget.Std(), Clock: time.Now}
}

// enabledModules builds the modules that the server config enables.
func (a App) enabledModules(cfg *config.Config) modules.Set {
	return a.Modules(settingsOf(cfg)).Enabled(cfg.ModuleEnabled)
}

// anchorPolicy reads the PATHPOLICY of a project from its default branch,
// the anchor of the next push. The policy is nil when the project has none.
func anchorPolicy(ctx context.Context, cfg *config.Config, api *gitlab.Client, project string) (gitlab.Project, *path.Policy, error) {
	p, err := api.Project(ctx, project)
	if err != nil {
		return p, nil, fmt.Errorf("project %s: %w", project, err)
	}
	guard := path.New()
	if !cfg.ModuleEnabled(guard.Name()) {
		return p, nil, errors.New("the PATHPOLICY module is disabled in the server config")
	}
	if p.DefaultBranch == "" {
		return p, nil, nil
	}
	data, err := api.RawFile(ctx, p.ID, guard.File(), p.DefaultBranch, guard.MaxPolicySize())
	if gitlab.KindOf(err) == gitlab.NotFound {
		return p, nil, nil
	}
	if err != nil {
		return p, nil, err
	}
	pol, err := path.Parse(data)
	if err != nil {
		return p, nil, fmt.Errorf("%s on %s is invalid: %w", guard.File(), p.DefaultBranch, err)
	}
	return p, pol, nil
}

// runExplain implements "explain --project P --user U --path X [--ref R]":
// which path rules match and whether the user satisfies them. Exit 1 means
// the user may not change the path.
func (a App) runExplain(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config")
	project := fs.String("project", "", "project ID or full path")
	user := fs.String("user", "", "username")
	path := fs.String("path", "", "path in the repository")
	ref := fs.String("ref", "", "branch or tag; default: the default branch")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	if *project == "" || *user == "" || *path == "" {
		return fail(stdio.Err, "explain", errors.New("--project, --user and --path are required"), ExitUsage)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return fail(stdio.Err, "explain", err, ExitError)
	}
	api, err := apiClient(cfg, opsMaxCalls)
	if err != nil {
		return fail(stdio.Err, "explain", err, ExitError)
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	p, pol, err := anchorPolicy(ctx, cfg, api, *project)
	if err != nil {
		return fail(stdio.Err, "explain", err, ExitError)
	}
	out := &output{w: stdio.Out}
	if pol == nil {
		out.printf("%s has no PATHPOLICY on its default branch: no path is protected\n", p.PathWithNamespace)
		return withOutput(out.err, ExitOK)
	}
	defaultRef := "refs/heads/" + p.DefaultBranch
	target := fullRef(*ref, defaultRef)
	rules, st := pol.ExplainPath(ctx, strings.TrimPrefix(*path, "/"), target, *user, refs.New(defaultRef, p.ID, api, nil))
	if !st.IsConfirmed() {
		return fail(stdio.Err, "explain", fmt.Errorf("protected branches cannot be read: %s", st.Reason()), ExitError)
	}
	out.printf("%s, PATHPOLICY of %s; path %s, ref %s, user %s\n", p.PathWithNamespace, p.DefaultBranch, *path, target, *user)
	if len(rules) == 0 {
		out.printf("no path rule matches: anyone who may push to the ref may change it\n")
		return withOutput(out.err, ExitOK)
	}
	allowed := true
	for _, r := range rules {
		switch {
		case !r.Applies:
			out.printf("  %s (refs: %s): does not apply to %s\n", r.Name, r.Refs, target)
			continue
		case r.Allowed:
			out.printf("  %s (refs: %s): allowed; allow: %s\n", r.Name, r.Refs, strings.Join(r.Allow, ", "))
		default:
			allowed = false
			out.printf("  %s (refs: %s): NOT allowed; allow: %s\n", r.Name, r.Refs, strings.Join(r.Allow, ", "))
		}
	}
	if allowed {
		out.printf("result: %s may change %s on %s (as a person: a deploy key never may)\n", *user, *path, target)
		return withOutput(out.err, ExitOK)
	}
	out.printf("result: %s may not change %s on %s: every applying rule must allow the user\n", *user, *path, target)
	return withOutput(out.err, ExitError)
}

// fullRef turns a branch name into refs/heads/<name>; full refs stay.
func fullRef(ref, def string) string {
	switch {
	case ref == "":
		return def
	case strings.HasPrefix(ref, "refs/"):
		return ref
	}
	return "refs/heads/" + ref
}

// runAdmins implements "admins --project P": configured, effective and
// missing admins and whether Owner recovery is active.
func (a App) runAdmins(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("admins", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config")
	project := fs.String("project", "", "project ID or full path")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	if *project == "" {
		return fail(stdio.Err, "admins", errors.New("--project is required"), ExitUsage)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return fail(stdio.Err, "admins", err, ExitError)
	}
	api, err := apiClient(cfg, opsMaxCalls)
	if err != nil {
		return fail(stdio.Err, "admins", err, ExitError)
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	p, pol, err := anchorPolicy(ctx, cfg, api, *project)
	if err != nil {
		return fail(stdio.Err, "admins", err, ExitError)
	}
	out := &output{w: stdio.Out}
	if pol == nil {
		out.printf("%s has no PATHPOLICY on its default branch: a Maintainer may add the first one (bootstrap)\n", p.PathWithNamespace)
		return withOutput(out.err, ExitOK)
	}
	report, st := pol.AdminReport(ctx, identity.NewAuthorizer(api, p.ID, nil, nil))
	if !st.IsConfirmed() {
		return fail(stdio.Err, "admins", fmt.Errorf("GitLab did not answer: %s", st.Reason()), ExitError)
	}
	var effective, missing []string
	for _, ad := range report {
		state := "effective"
		switch {
		case !ad.Exists:
			state = "missing: no such user, or blocked or banned"
		case !ad.Active:
			state = "missing: not active"
		case !ad.Effective:
			state = fmt.Sprintf("missing: access level %d, Maintainer (40) is required", ad.Access)
		}
		if ad.Effective {
			effective = append(effective, ad.Username)
		} else {
			missing = append(missing, ad.Username)
		}
		out.printf("  %-30s %s\n", ad.Username, state)
	}
	out.printf("%s: configured %d, effective %d, missing %d\n", p.PathWithNamespace, len(report), len(effective), len(missing))
	if len(effective) == 0 {
		out.printf("recovery active: an Owner of the project may change PATHPOLICY\n")
	} else {
		out.printf("recovery not active\n")
	}
	return withOutput(out.err, ExitOK)
}

// Defaults of gitlab-repopolicy test: a project path of three segments is
// never a personal project, so the namespace is not looked up.
const (
	testProjectPath = "local/test/repository"
	testProjectID   = 1
)

// runTest implements "test --repo DIR --old A --new B --ref R --user U": the
// decision of the hook for one ref update without a push. The report goes to
// stderr, so that stdout holds only audit events (audit.stdout).
func (a App) runTest(ctx context.Context, args []string, env []string, stdio Stdio) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config")
	repo := fs.String("repo", "", "repository with the objects of the update")
	oldRev := fs.String("old", "", "old value of the ref; zeros to create it")
	newRev := fs.String("new", "", "new value of the ref; zeros to delete it")
	ref := fs.String("ref", "", "full ref name, for example refs/tags/v1.0.0")
	user := fs.String("user", "", "username of the pusher")
	glID := fs.String("gl-id", "", "GL_ID of the pusher, user-<id> or key-<id>; looked up by --user when a module needs identity")
	projectPath := fs.String("project", testProjectPath, "project path")
	projectID := fs.Int64("project-id", testProjectID, "project ID")
	protocol := fs.String("protocol", "ssh", "GL_PROTOCOL: ssh, http or web")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	if *repo == "" || *oldRev == "" || *newRev == "" || *ref == "" || *user == "" || *projectID <= 0 {
		return fail(stdio.Err, "test", errors.New("--repo, --old, --new, --ref and --user are required"), ExitUsage)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return fail(stdio.Err, "test", err, ExitError)
	}
	path := envValue(env, "PATH")
	dir, err := gitx.Discover(ctx, path, *repo)
	if err != nil {
		return fail(stdio.Err, "test", err, ExitError)
	}
	git, err := gitx.Open(gitx.Env{Dir: dir, Path: path})
	if err != nil {
		return fail(stdio.Err, "test", err, ExitError)
	}
	code, err := a.decide(ctx, cfg, git, testRequest{oldRev: *oldRev, newRev: *newRev, ref: *ref, user: *user, glID: *glID,
		project: *projectPath, projectID: *projectID, protocol: *protocol, dir: dir, path: path}, stdio)
	if cerr := git.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return fail(stdio.Err, "test", err, ExitError)
	}
	return code
}

type testRequest struct {
	oldRev, newRev, ref, user, glID, project, protocol, dir, path string
	projectID                                                     int64
}

func (a App) decide(ctx context.Context, cfg *config.Config, git *gitx.Repo, r testRequest, stdio Stdio) (int, error) {
	oldOID, err := resolve(ctx, git, r.oldRev)
	if err != nil {
		return 0, err
	}
	newOID, err := resolve(ctx, git, r.newRev)
	if err != nil {
		return 0, err
	}
	switch {
	case oldOID == "" && newOID == "":
		return 0, errors.New("--old and --new are both zeros")
	case oldOID == "":
		oldOID = strings.Repeat("0", len(newOID))
	case newOID == "":
		newOID = strings.Repeat("0", len(oldOID))
	}
	api, err := apiClient(cfg, cfg.MaxAPICalls)
	if err != nil {
		return 0, err
	}
	mods := a.enabledModules(cfg)
	if r.glID == "" && needsIdentity(mods.All()) {
		users, err := api.UsersByUsername(ctx, r.user)
		if err != nil || len(users) != 1 {
			return 0, fmt.Errorf("--gl-id is not given and user %s cannot be looked up: %v", r.user, err)
		}
		r.glID = "user-" + strconv.FormatInt(users[0].ID, 10)
	}
	w, err := a.auditWriter(cfg, stdio.Out)
	if err != nil {
		return 0, err
	}
	hookEnv := hookenv.Env{Principal: module.Principal{GLID: r.glID, Username: r.user}, Protocol: r.protocol,
		Repository: "project-" + strconv.FormatInt(r.projectID, 10), ProjectPath: r.project, ProjectID: r.projectID,
		Git: hookenv.GitEnv{Dir: r.dir, Path: r.path}}
	out := core.Run(ctx, core.Settings{Mode: cfg.Mode, Total: cfg.Timeouts.Total.Std(), Help: cfg.Help}, core.Deps{
		Root: mods.Root, Modules: mods.Modules, Git: git, API: api, Audit: &gitAudit{w: w, git: git, ctx: ctx}, Clock: time.Now,
		Breakglass: breakglass.Store{Dir: cfg.BreakglassDir, TrustedUID: a.system().TrustedUID()},
	}, core.Request{Env: hookEnv, Updates: []module.RefUpdate{{Old: oldOID, New: newOID, Ref: r.ref}}})
	write(stdio.Err, out.Stdout)
	write(stdio.Err, out.Stderr)
	if out.AuditErr != nil {
		return 0, out.AuditErr
	}
	if !out.Allow {
		return ExitError, nil
	}
	summary, err := allowedSummary(ctx, git, oldOID, newOID, out.Touched)
	if err != nil {
		return 0, err
	}
	write(stdio.Err, summary)
	return ExitOK, nil
}

// allowedSummary is the result line of an allowed test: whether a rule
// applied and how many commits the hook took as new. A branch created in a
// clone where its commits are on other branches has no new commits.
func allowedSummary(ctx context.Context, git *gitx.Repo, oldOID, newOID string, touched bool) (string, error) {
	if module.IsZero(newOID) {
		return "allowed: the ref is deleted\n", nil
	}
	o := gitx.RevListOptions{Include: []string{newOID}, NotBranchesTags: true, Max: maxRangeCommits}
	if !module.IsZero(oldOID) {
		o.Exclude = []string{oldOID}
	}
	commits, err := git.RevList(ctx, o)
	if err != nil {
		return "", err
	}
	what := "no rule was touched"
	if touched {
		what = "the rules allow the change"
	}
	line := fmt.Sprintf("allowed: %s, %d new commits\n", what, len(commits))
	if len(commits) == 0 && module.IsZero(oldOID) {
		line += "  the commits of the new ref are already on other refs of this repository, so the hook takes none as new;\n" +
			"  to check them as a push does, run test in a mirror copy of the project (git clone --mirror)\n"
	}
	return line, nil
}

// resolve returns the object a revision names, "" for all zeros.
func resolve(ctx context.Context, git *gitx.Repo, rev string) (string, error) {
	if module.IsZero(rev) {
		return "", nil
	}
	if strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("invalid revision %q", rev)
	}
	info, err := git.Info(ctx, rev)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rev, err)
	}
	return info.OID, nil
}

func needsIdentity(mods []module.Module) bool {
	for _, m := range mods {
		if m.Needs().Identity {
			return true
		}
	}
	return false
}
