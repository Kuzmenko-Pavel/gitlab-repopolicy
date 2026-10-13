package cli

import (
	"context"
	"flag"
	"io"
)

func runVersion(_ context.Context, args []string, _ []string, stdio Stdio) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		out := &output{w: stdio.Err}
		out.printf("gitlab-repopolicy version: unexpected arguments %q\n", fs.Args())
		return withOutput(out.err, ExitUsage)
	}
	return withOutput(printVersion(stdio.Out, CurrentBuild()), ExitOK)
}

func printVersion(w io.Writer, b Build) error {
	out := &output{w: w}
	out.printf("gitlab-repopolicy %s\ncommit: %s\ndate: %s\ngo: %s\n", b.Version, b.Commit, b.Date, b.Go)
	return out.err
}
