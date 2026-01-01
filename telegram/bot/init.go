package bot

import (
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-auto-switch-dns-bot/cloudflare"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/utils"
)

func InitBot(bot *tgbotapi.BotAPI) {
	// 0️⃣ 初始化 Cloudflare 全局客户端
	if err := cloudflare.InitGlobalClient(); err != nil {
		utils.Logger.Warnf("⚠️ Cloudflare 客户端初始化失败: %v", err)
	}

	// 1️⃣ 先初始化命令列表，打破循环依赖
	InitCommands()
	// 2️⃣ 注册命令菜单
	if err := RegisterCommands(bot); err != nil {
		utils.Logger.Errorf("注册命令失败: %v", err)
	}

	// 3️⃣ 启动 Update 分发（可用 goroutine）
	go func() {
		u := tgbotapi.NewUpdate(0)
		u.Timeout = 60
		updates := bot.GetUpdatesChan(u)

		for update := range updates {
			HandleUpdate(bot, update)
		}
	}()

	// 4️⃣ 启动自动检测任务
	go StartAutoCheck(bot, time.Duration(config.Global.AutoCheck.CheckTime)*time.Minute)

	utils.Logger.Infof("Bot 初始化完成")
}
