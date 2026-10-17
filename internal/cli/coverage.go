package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/coverage"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

// runCoverage implements "coverage --token-file FILE": an
// administrator's token lists every top-level group; the token is only read.
// Gaps are exit code 1.
func (a App) runCoverage(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config: gitlab_url and the service account token")
	adminToken := fs.String("token-file", "", "file with a token of an administrator, scope read_api")
	timeout := fs.Duration("timeout", 10*time.Minute, "deadline of the whole check")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	if *adminToken == "" {
		return fail(stdio.Err, "coverage", errors.New("--token-file is required"), ExitUsage)
	}
	cfg, err := a.loadConfig(*configPath)
	if err != nil {
		return fail(stdio.Err, "coverage", err, ExitError)
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	sa, err := apiClient(cfg, 10)
	if err != nil {
		return fail(stdio.Err, "coverage", err, ExitError)
	}
	account, err := sa.CurrentUser(ctx)
	if err != nil {
		return fail(stdio.Err, "coverage", fmt.Errorf("the service account token: %w", err), ExitError)
	}
	admin, err := apiClientWithToken(cfg, *adminToken, opsMaxCalls)
	if err != nil {
		return fail(stdio.Err, "coverage", err, ExitError)
	}
	me, err := admin.CurrentUser(ctx)
	if err != nil {
		return fail(stdio.Err, "coverage", fmt.Errorf("--token-file: %w", err), ExitError)
	}
	if !me.IsAdmin {
		return fail(stdio.Err, "coverage", fmt.Errorf("--token-file: %s is not an administrator; only an administrator sees every group", me.Username), ExitError)
	}
	var files []string
	for _, m := range a.Modules(modules.Settings{Clock: time.Now}).All() {
		files = append(files, m.File())
	}
	rep, err := coverage.Run(ctx, admin, account.ID, files)
	if err != nil {
		return fail(stdio.Err, "coverage", err, ExitError)
	}
	out := &output{w: stdio.Out}
	for _, g := range rep.Gaps {
		role := "not a member"
		if g.Access > 0 {
			role = fmt.Sprintf("access level %d", g.Access)
		}
		out.printf("%s: %s is %s, Maintainer is required\n", g.Group.FullPath, account.Username, role)
		if len(g.PolicyProjects) > 0 {
			out.printf("  projects with policy files, their protected changes are denied: %s\n", strings.Join(g.PolicyProjects, ", "))
		}
	}
	if len(rep.Gaps) > 0 {
		out.printf("%d of %d top-level groups need %s as a Maintainer\n", len(rep.Gaps), rep.Groups, account.Username)
		return withOutput(out.err, ExitError)
	}
	out.printf("%s is a Maintainer in all %d top-level groups\n", account.Username, rep.Groups)
	return withOutput(out.err, ExitOK)
}
