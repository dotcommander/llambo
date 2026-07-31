package cmd

import "runtime/debug"

// Version can be set with -ldflags "-X github.com/dotcommander/llambo/cmd.Version=vX.Y.Z".
var Version string

// VersionString returns the installed module version, or the explicit build override.
func VersionString() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func isVersionFlag(args []string) bool {
	return len(args) == 1 && (args[0] == "-v" || args[0] == "--version")
}
