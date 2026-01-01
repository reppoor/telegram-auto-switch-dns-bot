package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// =======================
// 全局配置变量
// =======================
var (
	Global *Config   // 全局可访问
	once   sync.Once // 单例保护
)

// =======================
// 测试文件控制
// =======================
var testsStatus = 0

func getConfigFilePath(testsStatus int) string {
	if testsStatus == 1 {
		return "/tests_conf.yaml"
	}
	return "/conf.yaml"
}

type LoggerConfig struct {
	Level       string `yaml:"level"`
	FilePath    string `yaml:"file_path"`
	Development bool   `yaml:"development"`
	KeepDays    int    `yaml:"keep_days"`
}

// StartConfig =======================
type StartConfig struct {
	Models int `yaml:"models"`
}

// BackendListenConfig =======================
type BackendListenConfig struct {
	Host           string        `yaml:"host"`
	Port           string        `yaml:"port"`
	Key            string        `yaml:"key"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	MaxHeaderBytes int           `yaml:"max_header_bytes"`
	MaxRetries     int           `yaml:"max_retries"`
}

// AutoCheckConfig =======================
type AutoCheckConfig struct {
	CheckTime int `yaml:"check_time"`
	ApiFail   int `yaml:"api_fail"`
}

// DatabaseConfig =======================
type DatabaseConfig struct {
	Type     int    `yaml:"type"`
	File     string `yaml:"file"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	Name     string `yaml:"name"`
	Charset  string `yaml:"charset"`
}

// CloudflareConfig =======================
type CloudflareConfig struct {
	ApiToken string `yaml:"api_token"`
	TTL      int    `yaml:"ttl"`
}

// TelegramConfig =======================
type TelegramConfig struct {
	Id          int64  `yaml:"id"`
	Token       string `yaml:"token"`
	ApiEndpoint string `yaml:"apiEndpoint"`
	Key         string `yaml:"key"`
}

// BackendURL config for bot calling backend API
type BackendURL struct {
	Api     string        `yaml:"api"`
	Timeout time.Duration `yaml:"timeout"`
}

// NetworkConfig =======================
type NetworkConfig struct {
	EnableProxy bool   `yaml:"enabled"`
	Proxy       string `yaml:"proxy"`
}

// Config =======================
type Config struct {
	LoggerConfig  LoggerConfig        `yaml:"logger"`
	Start         StartConfig         `yaml:"start"`
	BackendListen BackendListenConfig `yaml:"backend_listen"`
	AutoCheck     AutoCheckConfig     `yaml:"auto_check"`
	Database      DatabaseConfig      `yaml:"database"`
	Cloudflare    CloudflareConfig    `yaml:"cloudflare"`
	Telegram      TelegramConfig      `yaml:"telegram"`
	Network       NetworkConfig       `yaml:"network"`

	BackendURL BackendURL `yaml:"backend_url"`
}

// LoadConfig =======================
// 加载配置（单例 + 全局）
// =======================
func LoadConfig(filePath string) *Config {
	once.Do(func() {
		cfg, err := loadConfigInternal(filePath)
		if err != nil {
			log.Fatalf("加载配置文件失败: %v", err)
		}
		Global = cfg
	})
	return Global
}

// 内部加载函数
func loadConfigInternal(filePath string) (*Config, error) {
	var fullPath string

	// 如果 filePath 为空或传 "1"，使用默认路径
	if filePath == "" || filePath == "1" {
		exePath, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("无法获取可执行文件路径: %v", err)
		}
		filePath = filepath.Join(filepath.Dir(exePath), getConfigFilePath(testsStatus))

		// 查找项目根目录
		workingDir, _ := os.Getwd()
		rootDir, err := findProjectRoot(workingDir)
		if err != nil {
			return nil, fmt.Errorf("项目根目录未找到: %v", err)
		}

		fullPath = filepath.Join(rootDir, getConfigFilePath(testsStatus))
	} else {
		// 使用传入的文件路径
		fullPath = filePath
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置文件 %s: %v", fullPath, err)
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %v", fullPath, err)
	}

	return &cfg, nil
}

// =======================
// 查找项目根目录（含 go.mod）
// =======================
func findProjectRoot(startDir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(startDir, "conf.yaml")); err == nil {
			return startDir, nil
		}
		parentDir := filepath.Dir(startDir)
		if parentDir == startDir {
			break
		}
		startDir = parentDir
	}
	return "", fmt.Errorf("项目根目录未找到")
}
