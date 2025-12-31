package mud

import (
	"os"
	"runtime/debug"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogConfig describes mud logging options.
type LogConfig struct {
	Level     string
	Format    string
	Output    string
	AddSource bool
	UTC       bool
	Color     bool
}

// NewLogger creates a structured logger for mud.
func NewLogger(cfg LogConfig) *zap.Logger {
	level := zapcore.InfoLevel
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = zapcore.DebugLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	}

	output := "stdout"
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		output = "stderr"
	case "", "stdout":
		output = "stdout"
	default:
		output = "stdout"
	}

	encCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	if cfg.UTC {
		encCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.UTC().Format(time.RFC3339Nano))
		}
	} else {
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	format := strings.ToLower(cfg.Format)
	if format == "json" {
		encCfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	} else if cfg.Color {
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
	}

	var encoder zapcore.Encoder
	if format == "json" {
		encoder = zapcore.NewJSONEncoder(encCfg)
	} else {
		encoder = zapcore.NewConsoleEncoder(encCfg)
	}

	ws := zapcore.AddSync(selectOutput(output))
	core := zapcore.NewCore(encoder, ws, level)
	options := []zap.Option{}
	if cfg.AddSource {
		options = append(options, zap.AddCaller())
	}
	logger := zap.New(core, options...)
	version, commit := buildVersion()
	return logger.With(
		zap.String("app", "mud"),
		zap.Int("pid", os.Getpid()),
		zap.String("version", version),
		zap.String("commit", commit),
	)
}

func selectOutput(output string) *os.File {
	if output == "stderr" {
		return os.Stderr
	}
	return os.Stdout
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
