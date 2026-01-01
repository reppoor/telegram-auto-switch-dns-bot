package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-auto-switch-dns-bot/utils"
)

func RegisterCommands(bot *tgbotapi.BotAPI) error {
	utils.Logger.Info("📝 正在注册 Telegram 命令...")

	var tgCommands []tgbotapi.BotCommand
	for _, cmd := range Commands {
		tgCommands = append(tgCommands, tgbotapi.BotCommand{
			Command:     cmd.Command,
			Description: cmd.Description,
		})
		utils.Logger.Infof("📌 命令已加载: /%s - %s", cmd.Command, cmd.Description)
	}

	config := tgbotapi.NewSetMyCommands(tgCommands...)
	_, err := bot.Request(config)
	if err != nil {
		utils.Logger.Errorf("❌ 注册命令失败: %v", err)
		return err
	}

	utils.Logger.Info("✅ Telegram 命令注册成功！")
	return nil
}
