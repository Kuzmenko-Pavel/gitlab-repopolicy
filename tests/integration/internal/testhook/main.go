// Command testhook is gitlab-repopolicy with the test modules added after the
// default ones; the integration harness installs it as the pre-receive hook.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/cli"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/tests/integration/internal/testmodule"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	app := cli.App{Modules: func(s modules.Settings) []module.Module {
		return append(modules.Default(s), testmodule.Modules()...)
	}}
	code := app.Run(ctx, os.Args[1:], os.Environ(), cli.Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	stop()
	os.Exit(code)
}
