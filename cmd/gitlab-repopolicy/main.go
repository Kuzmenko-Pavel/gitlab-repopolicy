// Command gitlab-repopolicy is a server-side repository policy validator for GitLab.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], os.Environ(), cli.Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	stop()
	os.Exit(code)
}
