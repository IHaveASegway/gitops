// Package buildinfo reports the version of the running binary.
package buildinfo

import "runtime/debug"

// version is injected at build time:
//
//	go build -ldflags "-X github.com/IHaveASegway/gitops/internal/buildinfo.version=v1.2.3"
var version string

// Version returns the release version: the value injected at build time,
// otherwise the module version recorded by `go install pkg@version`,
// otherwise "dev".
func Version() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
