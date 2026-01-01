package utils

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os"
	"path/filepath"
	"strings"
	"telegram-auto-switch-dns-bot/config"
	"time"
)

var Logger *zap.SugaredLogger
var logDir string

// 初始化日志器
func InitLogger() {
	cfg := config.Global.LoggerConfig
	isDev := cfg.Development || cfg.Level == "debug"

	// 日志目录
	logDir = filepath.Dir(cfg.FilePath)
	if logDir == "." || logDir == "" {
		logDir = "logs"
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("❌ Failed to create log directory: %v", err)
	}

	// 初始化日志文件
	setupLogger(cfg, isDev)

	// 启动每日轮换任务
	go scheduleDailyRotation(cfg, isDev)
}

// 初始化并创建日志文件
func setupLogger(cfg config.LoggerConfig, isDev bool) {
	currentDate := time.Now().Format("2006-01-02")
	logFile := filepath.Join(logDir, fmt.Sprintf("app-%s.log", currentDate))

	// 删除旧日志
	retention := cfg.KeepDays
	if retention <= 0 {
		retention = 2 // 默认保留 2 天
	}
	cleanupOldLogs(logDir, retention)

	var zapCfg zap.Config
	if isDev {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}

	zapCfg.Encoding = "console"
	zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	zapCfg.EncoderConfig.TimeKey = "time"
	zapCfg.EncoderConfig.LevelKey = "level"
	zapCfg.EncoderConfig.MessageKey = "msg"
	zapCfg.EncoderConfig.CallerKey = ""
	zapCfg.DisableStacktrace = true

	// 设置日志级别
	lvl := zapcore.InfoLevel
	if err := lvl.UnmarshalText([]byte(cfg.Level)); err != nil {
		log.Printf("[Warning] invalid log level '%s', fallback to info", cfg.Level)
	}
	zapCfg.Level = zap.NewAtomicLevelAt(lvl)

	// Windows 环境下避免 stdout 管道关闭错误
	if isDev {
		zapCfg.OutputPaths = []string{"stdout", logFile}
		zapCfg.ErrorOutputPaths = []string{"stderr", logFile}
	} else {
		// 生产环境只写文件，避免管道错误
		zapCfg.OutputPaths = []string{logFile}
		zapCfg.ErrorOutputPaths = []string{logFile}
	}

	logger, err := zapCfg.Build()
	if err != nil {
		log.Fatalf("❌ Failed to initialize logger: %v", err)
	}

	if Logger != nil {
		// 优雅关闭旧 logger，忽略管道关闭错误
		_ = Logger.Sync()
	}
	Logger = logger.Sugar()

	Logger.Infof("✅ Logger initialized. Level=%s, Dev=%v, File=%s, KeepDays=%d", cfg.Level, isDev, logFile, retention)
}

// 每天 0 点定时切换日志文件
func scheduleDailyRotation(cfg config.LoggerConfig, isDev bool) {
	for {
		now := time.Now()
		nextMidnight := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
		sleepDuration := nextMidnight.Sub(now)

		Logger.Infof("🕛 Next log rotation at: %s", nextMidnight.Format(time.RFC3339))
		time.Sleep(sleepDuration)

		setupLogger(cfg, isDev)
	}
}

// 删除超过 days 天的日志文件
func cleanupOldLogs(dir string, days int) {
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("[Warning] failed to read log directory: %v", err)
		return
	}

	expireTime := time.Now().AddDate(0, 0, -days)
	for _, f := range files {
		if f.IsDir() {
			continue
		}

		name := f.Name()
		if !strings.HasPrefix(name, "app-") || !strings.HasSuffix(name, ".log") {
			continue
		}

		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, "app-"), ".log")
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		if t.Before(expireTime) {
			fullPath := filepath.Join(dir, name)
			if err := os.Remove(fullPath); err == nil {
				log.Printf("🧹 Deleted old log file: %s", fullPath)
			}
		}
	}
}

// 简化方法
func Info(msg string, args ...interface{})  { Logger.Infof(msg, args...) }
func Warn(msg string, args ...interface{})  { Logger.Warnf(msg, args...) }
func Error(msg string, args ...interface{}) { Logger.Errorf(msg, args...) }
func Debug(msg string, args ...interface{}) { Logger.Debugf(msg, args...) }
func Sync()                                 { _ = Logger.Sync() }
