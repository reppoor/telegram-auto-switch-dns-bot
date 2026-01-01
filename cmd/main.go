package main

import (
	"github.com/fatih/color"
	"os"
	"os/signal"
	"syscall"
	"telegram-auto-switch-dns-bot/CheckBackend"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/db"

	"telegram-auto-switch-dns-bot/telegram/bot"
	"telegram-auto-switch-dns-bot/utils"
)

func main() {
	// 加载配置与日志
	Config := config.LoadConfig("")
	utils.InitLogger()
	defer utils.Logger.Sync()

	// 设置优雅关闭
	setupGracefulShutdown()

	// 打印启动信息
	color.Cyan("========================================")
	color.Cyan("  Telegram Auto Switch DNS Bot v1.0.0")
	color.Cyan("========================================")
	utils.Logger.Infof("程序启动，当前模式: %d", Config.Start.Models)

	// 根据模式启动服务
	switch Config.Start.Models {
	case 1:
		color.Green("🟢 启动模式1：仅BOT端")
		if err := initBotServices(); err != nil {
			return
		}
		bot.TelegramApp()
		select {}
	case 2:
		color.Yellow("🟡 启动模式2：仅检测端")
		CheckBackend.CheckApi()
	case 3:
		color.Cyan("🔵 启动模式3：完整模式")
		if err := initBotServices(); err != nil {
			return
		}
		go CheckBackend.CheckApi()
		bot.TelegramApp()
		select {}
	default:
		color.Red("🔴 未知模式: %d，请检查配置文件", Config.Start.Models)
	}
}

// setupGracefulShutdown 设置优雅关闭
func setupGracefulShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		utils.Logger.Info("📢 接收到停止信号，正在优雅关闭...")
		db.CloseDB()
		utils.Logger.Sync()
		os.Exit(0)
	}()
}

// initBotServices 初始化BOT所需服务
func initBotServices() error {

	if err := db.InitDB(); err != nil {
		utils.Logger.Errorf("数据库初始化失败: %v", err)
		color.Red("🔴 数据库初始化失败: %v", err)
		return err
	}

	utils.Logger.Info("数据库初始化成功")
	color.Green("🟢 数据库初始化成功")
	return nil
}
