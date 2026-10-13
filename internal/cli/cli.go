// Package cli implements the gitlab-repopolicy command line and acts as the composition root.
package cli

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/buildinfo"
)

// Exit codes returned by Run.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Stdio carries the standard streams of the process.
type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Build describes the binary being run.
type Build struct {
	Version string
	Commit  string
	Date    string
	Go      string
}

type command struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string, env []string, stdio Stdio) int
}

// Run executes the command line args (without the program name) and returns the exit code.
func Run(ctx context.Context, args []string, env []string, stdio Stdio) int {
	commands := commandList()
	if len(args) == 0 {
		return withOutput(usage(stdio.Err, commands), ExitUsage)
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		return withOutput(usage(stdio.Out, commands), ExitOK)
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(ctx, args[1:], env, stdio)
		}
	}
	out := &output{w: stdio.Err}
	out.printf("gitlab-repopolicy: unknown command %q\n\n", args[0])
	if out.err != nil {
		return ExitError
	}
	return withOutput(usage(stdio.Err, commands), ExitUsage)
}

func commandList() []command {
	return []command{
		{name: "version", summary: "print version, commit, build date and Go version", run: runVersion},
	}
}

func usage(w io.Writer, commands []command) error {
	out := &output{w: w}
	out.printf("Usage: gitlab-repopolicy <command> [flags]\n\nCommands:\n")
	for _, c := range commands {
		out.printf("  %-20s %s\n", c.name, c.summary)
	}
	return out.err
}

// CurrentBuild returns the metadata of the running binary.
func CurrentBuild() Build {
	info := buildinfo.Resolve()
	return Build{
		Version: info.Version,
		Commit:  info.Commit,
		Date:    info.Date,
		Go:      runtime.Version(),
	}
}

// output writes formatted text and keeps the first write error.
type output struct {
	w   io.Writer
	err error
}

func (o *output) printf(format string, a ...any) {
	if o.err != nil {
		return
	}
	_, o.err = fmt.Fprintf(o.w, format, a...)
}

// withOutput returns code, or ExitError if writing the command output failed.
func withOutput(err error, code int) int {
	if err != nil {
		return ExitError
	}
	return code
}
