package mud

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
)

// LogConfig describes mud logging options.
type LogConfig struct {
	Level     string
	Format    string
	Output    string
	AddSource bool
	UTC       bool
}

// NewLogger creates a structured logger for mud.
func NewLogger(cfg LogConfig) *slog.Logger {
	lvl := slog.LevelInfo
	switch strings.ToLower(cfg.Level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}

	writer := os.Stdout
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		writer = os.Stderr
	case "", "stdout":
	default:
		writer = os.Stdout
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: cfg.AddSource,
	}
	if cfg.UTC {
		opts.ReplaceAttr = func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				t := attr.Value.Time().UTC()
				return slog.Attr{Key: attr.Key, Value: slog.TimeValue(t)}
			}
			return attr
		}
	}

	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "json" {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		handler = slog.NewTextHandler(writer, opts)
	}

	logger := slog.New(handler)
	version, commit := buildVersion()
	return logger.With(
		"app", "mud",
		"pid", os.Getpid(),
		"version", version,
		"commit", commit,
	)
}

func buildVersion() (string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev", "unknown"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "dev"
	}
	commit := "unknown"
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			commit = setting.Value
			break
		}
	}
	return version, commit
}
