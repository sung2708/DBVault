package cli

import "runtime/debug"

// ResolveBuild combines explicit release linker metadata with Go's module/VCS
// metadata. A checkout stays a development build; a module install reports the
// actual module version. VCS time is not a build timestamp.
func ResolveBuild(b Build) Build {
	info, _ := debug.ReadBuildInfo()
	return resolveBuild(b, info)
}

func resolveBuild(b Build, info *debug.BuildInfo) Build {
	if b.Version == "" {
		b.Version = "dev"
	}
	if b.Commit == "" {
		b.Commit = "unknown"
	}
	if b.Date == "" {
		b.Date = "unknown"
	}
	if info == nil {
		return b
	}
	if b.Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		b.Version = info.Main.Version
	}
	if b.Commit == "unknown" {
		modified := false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				b.Commit = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
		if modified && b.Commit != "unknown" {
			b.Commit += "-dirty"
		}
	}
	return b
}
