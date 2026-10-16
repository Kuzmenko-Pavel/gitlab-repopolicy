package cli

import (
	"context"
	"flag"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/audit"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/settings"
)

// runSettingsCheck implements "settings-check": a drift
// is an audit settings_drift event and exit code 1, for alerting.
func (a App) runSettingsCheck(ctx context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("settings-check", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfig, "server config")
	timeout := fs.Duration("timeout", time.Hour, "deadline of the whole check")
	if !parse(fs, args, stdio.Err) {
		return ExitUsage
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return fail(stdio.Err, "settings-check", err, ExitError)
	}
	api, err := apiClient(cfg, opsMaxCalls)
	if err != nil {
		return fail(stdio.Err, "settings-check", err, ExitError)
	}
	w, err := a.auditWriter(cfg, stdio.Out)
	if err != nil {
		return fail(stdio.Err, "settings-check", err, ExitError)
	}
	log := logger(stdio.Err)
	mods := a.enabledModules(cfg)
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	start := time.Now()
	rep, err := settings.Run(ctx, api, settings.Options{Expected: cfg.SettingsCheck, Guard: mods.Root, Modules: mods.All()})
	if err != nil {
		return fail(stdio.Err, "settings-check", err, ExitError)
	}
	out := &output{w: stdio.Out}
	drifted := rep.Drifted()
	for _, p := range drifted {
		ev := audit.Event{Event: audit.EventSettingsDrift, ProjectID: p.ID, ProjectPath: p.Path, APICalls: api.Calls()}
		for _, d := range p.Drifts {
			out.printf("%s: %s: %s\n", p.Path, d.Check, d.Reason)
			ev.Violations = append(ev.Violations, audit.Violation{Rule: d.Check, Reason: d.Reason})
		}
		if err := w.Write(ev); err != nil {
			log.Error("audit", "err", err)
			return withOutput(out.err, ExitError)
		}
	}
	log.Info("settings-check done", "checked", len(rep.Checked), "drifted", len(drifted), "skipped", rep.Skipped,
		"api_calls", api.Calls(), "duration", time.Since(start).Round(time.Millisecond))
	if len(drifted) > 0 {
		return withOutput(out.err, ExitError)
	}
	out.printf("%d projects with PATHPOLICY checked, no drift\n", len(rep.Checked))
	return withOutput(out.err, ExitOK)
}
