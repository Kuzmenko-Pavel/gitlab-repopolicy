// Package buildinfo holds build metadata injected at link time with -ldflags.
package buildinfo

// Version is the product version without the "v" prefix, for example "1.0.0".
var Version string

// Commit is the abbreviated commit hash the binary was built from.
var Commit string

// Date is the commit date in RFC 3339 format; it keeps builds reproducible.
var Date string
