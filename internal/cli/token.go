package cli

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/config"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/token"
)

// tokenRotateTimeout bounds token rotate.
const tokenRotateTimeout = time.Minute

// runToken implements "token rotate [--before 30d]": only
// root, by a timer. A failure is an audit internal_error and exit code 1, for
// alerting; when GitLab refuses, the current token stays valid.
func (a App) runToken(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	if len(args) == 0 || args[0] != "rotate" {
		return fail(stdio.Err, "token", errors.New("expected rotate"), ExitUsage)
	}
	fs := flag.NewFlagSet("token rotate", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config")
	before := config.Duration(RotateBefore)
	fs.TextVar(&before, "before", config.Duration(RotateBefore), "rotate when the token expires within this time, for example 30d")
	if !parse(fs, args[1:], stdio.Err) {
		return ExitUsage
	}
	if !a.system().IsRoot() {
		return fail(stdio.Err, "token rotate", errNotRoot, ExitError)
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return fail(stdio.Err, "token rotate", err, ExitError)
	}
	log := logger(stdio.Err)
	api, err := apiClient(cfg, 10)
	if err != nil {
		return fail(stdio.Err, "token rotate", err, ExitError)
	}
	ctx, cancel := context.WithTimeout(ctx, tokenRotateTimeout)
	defer cancel()
	sys := a.system()
	res, err := token.Rotate(ctx, api, token.Options{File: cfg.TokenFile, Before: before.Std(), Lifetime: cfg.TokenLifetime.Std(),
		Now: time.Now(), Chown: sys.Chown})
	if err != nil {
		reason := "token rotate: " + err.Error()
		if w, werr := a.auditWriter(cfg, stdio.Out); werr != nil {
			log.Error("audit", "err", werr)
		} else if werr := w.Write(audit.Event{Event: audit.EventInternalError, Action: "token_rotate", Reason: reason}); werr != nil {
			log.Error("audit", "err", werr)
		}
		return fail(stdio.Err, "token rotate", err, ExitError)
	}
	out := &output{w: stdio.Out}
	if !res.Rotated {
		out.printf("the token expires on %s, more than %s from now: not rotated\n", res.OldExpiry, before)
		return withOutput(out.err, ExitOK)
	}
	log.Info("token rotated", "file", cfg.TokenFile, "old_expiry", res.OldExpiry, "new_expiry", res.NewExpiry)
	out.printf("the token is rotated: it expires on %s\n", res.NewExpiry)
	return withOutput(out.err, ExitOK)
}
