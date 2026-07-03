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
	Level        string
	ModuleLevels map[string]string
	Format       string
	Output       string
	AddSource    bool
	UTC          bool
	Color        bool
}

// ModuleLoggerFactory creates loggers with per-module log levels.
type ModuleLoggerFactory struct {
	baseLogger   *zap.Logger
	baseLevel    zapcore.Level
	moduleLevels map[string]zapcore.Level
	encoder      zapcore.Encoder
	writeSyncer  zapcore.WriteSyncer
	options      []zap.Option
}

// NewModuleLoggerFactory creates a factory for module-specific loggers.
func NewModuleLoggerFactory(cfg LogConfig) *ModuleLoggerFactory {
	baseLevel := parseLevel(cfg.Level)
	moduleLevels := make(map[string]zapcore.Level)
	for module, levelStr := range cfg.ModuleLevels {
		moduleLevels[module] = parseLevel(levelStr)
	}

	output := "stdout"
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		output = "stderr"
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
	var options []zap.Option
	if cfg.AddSource {
		options = append(options, zap.AddCaller())
	}

	// Create base logger with default level
	core := zapcore.NewCore(encoder, ws, baseLevel)
	baseLogger := zap.New(core, options...)
	version, commit := buildVersion()
	baseLogger = baseLogger.With(
		zap.String("app", "mud"),
		zap.Int("pid", os.Getpid()),
		zap.String("version", version),
		zap.String("commit", commit),
	)

	return &ModuleLoggerFactory{
		baseLogger:   baseLogger,
		baseLevel:    baseLevel,
		moduleLevels: moduleLevels,
		encoder:      encoder,
		writeSyncer:  ws,
		options:      options,
	}
}

// Logger returns the base logger (for non-module use).
func (f *ModuleLoggerFactory) Logger() *zap.Logger {
	return f.baseLogger
}

// ModuleLogger returns a logger for a specific module with its configured level.
// If no specific level is configured for the module, it uses the base level.
func (f *ModuleLoggerFactory) ModuleLogger(module string) *zap.Logger {
	level, hasOverride := f.moduleLevels[module]
	if !hasOverride {
		// No override, use base logger with module field
		return f.baseLogger.With(zap.String("module", module))
	}

	// Create a new core with the module-specific level
	core := zapcore.NewCore(f.encoder, f.writeSyncer, level)
	logger := zap.New(core, f.options...)
	version, commit := buildVersion()
	return logger.With(
		zap.String("app", "mud"),
		zap.Int("pid", os.Getpid()),
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("module", module),
	)
}

// parseLevel converts a string to a zapcore.Level.
func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(s) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// NewLogger creates a structured logger for mud.
// Deprecated: Use NewModuleLoggerFactory for per-module log levels.
func NewLogger(cfg LogConfig) *zap.Logger {
	return NewModuleLoggerFactory(cfg).Logger()
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
