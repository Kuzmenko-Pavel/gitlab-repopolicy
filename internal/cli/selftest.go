package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/breakglass"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

// RequiredScopes are the scopes of the service account token, exactly (architecture §13.2).
var RequiredScopes = []string{"read_api", "self_rotate"}

// RotateBefore is the default of token rotate --before; selftest warns from then on.
const RotateBefore = 30 * 24 * time.Hour

// selftestTimeout bounds selftest.
const selftestTimeout = 30 * time.Second

// checks collects selftest results.
type checks struct {
	out    *output
	failed bool
}

func (c *checks) ok(name, format string, a ...any) { c.line("ok", name, format, a...) }
func (c *checks) warn(name, format string, a ...any) {
	c.line("warn", name, format, a...)
}
func (c *checks) info(name, format string, a ...any) { c.line("info", name, format, a...) }
func (c *checks) fail(name, format string, a ...any) {
	c.failed = true
	c.line("FAIL", name, format, a...)
}
func (c *checks) line(level, name, format string, a ...any) {
	c.out.printf("%-4s %-11s %s\n", level, name, fmt.Sprintf(format, a...))
}

// runSelftest implements "selftest [--config FILE]": config, token and what it
// may see, token expiry, API, audit log; the git version is informational,
// because selftest does not run in the environment of Gitaly (architecture §12.1).
func (a App) runSelftest(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("selftest", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(ctx, selftestTimeout)
	defer cancel()
	c := &checks{out: &output{w: stdio.Out}}
	c.info("version", "gitlab-repopolicy %s", CurrentBuild().Version)
	cfg, err := loadConfig(*configPath)
	if err != nil {
		c.fail("config", "%s: %v", *configPath, err)
		return withOutput(c.out.err, ExitError)
	}
	c.ok("config", "%s, mode %s", *configPath, cfg.Mode)
	a.checkModules(c, cfg)
	a.checkAudit(c, cfg)
	a.checkBreakglass(c, cfg)
	checkToken(ctx, c, cfg)
	if out, err := exec.CommandContext(ctx, "git", "--version").Output(); err == nil {
		c.info("git", "%s in PATH of selftest; the hook uses the git of Gitaly", strings.TrimSpace(string(out)))
	} else {
		c.info("git", "not in PATH of selftest (%v); the hook uses the git of Gitaly", err)
	}
	if c.failed {
		return withOutput(c.out.err, ExitError)
	}
	return withOutput(c.out.err, ExitOK)
}

func (a App) checkModules(c *checks, cfg *config.Config) {
	var names []string
	for _, m := range a.Modules(modules.Settings{CommitPresets: cfg.CommitPolicyPresets,
		PatternTimeout: cfg.Timeouts.Pattern.Std(), PatternBudget: cfg.Timeouts.PatternBudget.Std(), Clock: time.Now}) {
		if !cfg.ModuleEnabled(m.Name()) {
			continue
		}
		names = append(names, m.Name())
		if r, ok := m.(module.ConfigReporter); ok {
			for _, e := range r.ConfigErrors() {
				c.fail("modules", "%s", e)
			}
		}
	}
	if len(names) == 0 {
		c.warn("modules", "all modules are disabled")
		return
	}
	c.ok("modules", "enabled: %s", strings.Join(names, ", "))
}

func (a App) checkAudit(c *checks, cfg *config.Config) {
	if cfg.Audit.File == "" {
		c.fail("audit", "audit.file is required: the hook does not write audit to stdout")
		return
	}
	dir, err := os.Stat(filepath.Dir(cfg.Audit.File))
	if err != nil {
		c.fail("audit", "log directory: %v", err)
		return
	}
	dirOwner := ownerOf(dir)
	info, err := os.Lstat(cfg.Audit.File)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.ok("audit", "%s does not exist yet; the directory belongs to %s", cfg.Audit.File, dirOwner)
		return
	case err != nil:
		c.fail("audit", "%v", err)
		return
	case !info.Mode().IsRegular():
		c.fail("audit", "%s is not a regular file", cfg.Audit.File)
		return
	}
	f, err := os.OpenFile(cfg.Audit.File, os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // G304: path comes from the server config
	if err != nil {
		c.fail("audit", "%s is not writable: %v", cfg.Audit.File, err)
		return
	}
	if err := f.Close(); err != nil {
		c.fail("audit", "%v", err)
		return
	}
	if owner := ownerOf(info); owner != dirOwner {
		c.warn("audit", "%s belongs to %s, the directory to %s: the hook may not be able to append", cfg.Audit.File, owner, dirOwner)
		return
	}
	c.ok("audit", "%s, owner %s", cfg.Audit.File, ownerOf(info))
}

func ownerOf(info fs.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", st.Uid, st.Gid)
	}
	return "unknown"
}

func (a App) checkBreakglass(c *checks, cfg *config.Config) {
	store := breakglass.Store{Dir: cfg.BreakglassDir, TrustedUID: a.system().TrustedUID()}
	entries, err := store.List()
	if err != nil {
		c.warn("breakglass", "%v: break-glass cannot be granted", err)
		return
	}
	var active []string
	for _, e := range entries {
		if e.Err == nil && e.Grant.Valid(time.Now()) {
			active = append(active, e.Name)
		}
	}
	if len(active) > 0 {
		c.warn("breakglass", "active for projects %s", strings.Join(active, ", "))
		return
	}
	c.ok("breakglass", "%s, no active grants", cfg.BreakglassDir)
}

// checkToken checks the token file and the service account behind it (architecture §13.2).
func checkToken(ctx context.Context, c *checks, cfg *config.Config) {
	info, err := os.Stat(cfg.TokenFile)
	if err != nil {
		c.fail("token", "%v", err)
		return
	}
	if info.Mode().Perm()&0o007 != 0 {
		c.fail("token", "%s is accessible to others (%s); expected root:<git-user> 0440", cfg.TokenFile, info.Mode().Perm())
	}
	api, err := apiClient(cfg, 10)
	if err != nil {
		c.fail("api", "%v", err)
		return
	}
	u, err := api.CurrentUser(ctx)
	if err != nil {
		c.fail("api", "GET /user: %v", err)
		return
	}
	c.ok("api", "%s reachable as %s (id %d)", cfg.GitLabURL, u.Username, u.ID)
	switch {
	case u.IsAdmin:
		c.fail("account", "%s is an administrator; the hook needs an instance service account", u.Username)
	case !u.Bot:
		c.fail("account", "%s is not a service account (bot: false)", u.Username)
	case !u.External:
		c.fail("account", "%s is not external; instance service accounts are", u.Username)
	default:
		c.ok("account", "%s is a service account", u.Username)
	}
	t, err := api.TokenSelf(ctx)
	if err != nil {
		c.fail("token", "GET /personal_access_tokens/self: %v", err)
		return
	}
	scopes := slices.Sorted(slices.Values(t.Scopes))
	if !slices.Equal(scopes, RequiredScopes) {
		c.fail("token", "scopes %s; exactly %s are required", strings.Join(scopes, ", "), strings.Join(RequiredScopes, ", "))
	}
	if !t.Active || t.Revoked {
		c.fail("token", "the token is not active")
		return
	}
	expires, err := time.Parse(time.DateOnly, t.ExpiresAt)
	if err != nil {
		c.fail("token", "no expiry date (%q); rotate the token with an expiry", t.ExpiresAt)
		return
	}
	// GitLab revokes the token at the start of the expiry date, UTC.
	left := time.Until(expires)
	if left < RotateBefore {
		c.warn("token", "expires on %s, in %d days: run gitlab-repopolicy token rotate", t.ExpiresAt, int(left.Hours()/24))
		return
	}
	c.ok("token", "scopes %s, expires on %s", strings.Join(scopes, ", "), t.ExpiresAt)
}

// opsMaxCalls is the API call budget of the operational commands.
const opsMaxCalls = 1_000_000

// apiClient creates a client with the service account token of the config.
func apiClient(cfg *config.Config, maxCalls int) (*gitlab.Client, error) {
	return apiClientWithToken(cfg, cfg.TokenFile, maxCalls)
}

func apiClientWithToken(cfg *config.Config, tokenFile string, maxCalls int) (*gitlab.Client, error) {
	ep, err := cfg.Endpoint()
	if err != nil {
		return nil, err
	}
	return gitlab.New(gitlab.Options{Socket: ep.Socket, BaseURL: ep.BaseURL, CAFile: cfg.CAFile, TokenFile: tokenFile,
		Timeout: cfg.Timeouts.HTTP.Std(), MaxCalls: maxCalls, UserAgent: "gitlab-repopolicy/" + CurrentBuild().Version})
}
