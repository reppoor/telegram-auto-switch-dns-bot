package db

import (
	"fmt"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/utils"
	"time"
)

var DB *gorm.DB

// InitDB 初始化数据库连接，失败时记录日志并返回错误
func InitDB() error {
	cfg := config.Global.Database
	var dsn string
	var err error

	// 根据配置文件选择 MySQL 或 SQLite
	switch cfg.Type {
	case 2: // MySQL
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=%s",
			cfg.User,
			cfg.Password,
			cfg.Host,
			cfg.Port,
			cfg.Name,
			cfg.Charset,
		)
		DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})
	case 1: // SQLite
		dsn = cfg.File
		DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})
		if err == nil {
			// 启用 SQLite 外键约束（默认禁用）
			DB.Exec("PRAGMA foreign_keys = ON;")
			utils.Logger.Infof("SQLite 外键约束已启用")
		}
	default:
		errMsg := fmt.Sprintf("不支持的数据库类型: %v", cfg.Type)
		utils.Logger.Errorf(errMsg)
		return fmt.Errorf(errMsg)
	}

	if err != nil {
		utils.Logger.Errorf("无法连接数据库: %v", err)
		return fmt.Errorf("无法连接数据库: %w", err)
	}

	utils.Logger.Infof("数据库连接成功")

	// 设置 MySQL 连接池
	if cfg.Type == 2 {
		sqlDB, _ := DB.DB()
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// 自动迁移
	if err := AutoMigrate(); err != nil {
		utils.Logger.Errorf("自动迁移失败: %v", err)
		return fmt.Errorf("自动迁移失败: %w", err)
	}

	return nil
}

// AutoMigrate 自动迁移数据库模型，失败时返回错误
func AutoMigrate() error {
	if DB == nil {
		err := fmt.Errorf("数据库未初始化")
		utils.Logger.Errorf("自动迁移失败: %v", err)
		return err
	}

	err := DB.AutoMigrate(
		&models.DomainRecord{},
		&models.ForwardRecord{},
		&models.TelegramAdmins{},
	)
	if err != nil {
		utils.Logger.Errorf("自动迁移失败: %v", err)
		return err
	}

	utils.Logger.Infof("数据库模型迁移成功")
	return nil
}

// CloseDB 关闭数据库连接
func CloseDB() {
	sqlDB, err := DB.DB()
	if err != nil {
		utils.Logger.Errorf("获取数据库连接失败: %v", err)
	}
	err = sqlDB.Close()
	if err != nil {
		utils.Logger.Errorf("关闭数据库连接失败: %v", err)
	} else {
		utils.Logger.Infof("数据库连接已成功关闭")
	}
}
