package tui

// Build metadata. The release build script (scripts/build.ps1 or
// scripts/build.sh) injects these values with -ldflags, for example:
//
//	go build -ldflags "-X yhchat-tui/internal/tui.Version=1.2.3 \
//	  -X yhchat-tui/internal/tui.Commit=abc1234 \
//	  -X yhchat-tui/internal/tui.BuildTime=2026-11-14T02:00:00Z"
//
// Plain `go build` keeps the development defaults below.
var (
	// Version is the application version shown by `yhchat-tui -version`.
	Version = "0.1.0-dev"
	// Commit is the git commit the binary was built from.
	Commit = "unknown"
	// BuildTime is the UTC timestamp of the build.
	BuildTime = "unknown"
)
