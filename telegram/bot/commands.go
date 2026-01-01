package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"
)

// BotCommand 定义结构体，便于管理
type BotCommand struct {
	Command      string                     // 命令名，例如 "start"
	Description  string                     // 命令描述，例如 "启动机器人"
	Handler      func(update UpdateContext) // 处理函数
	RequireAdmin bool                       // 是否需要管理员权限
}

// UpdateContext 是自定义上下文
type UpdateContext struct {
	Update    tgbotapi.Update
	Bot       *tgbotapi.BotAPI
	Username  string
	LastName  string
	FirstName string
	UserID    int64
	MessageID int
	DB        *gorm.DB
}

var Commands []BotCommand // 先定义空切片

// InitCommands 延迟初始化命令列表
func InitCommands() {
	Commands = []BotCommand{
		{
			Command:      "start",
			Description:  "启动机器人",
			Handler:      startHandler,
			RequireAdmin: false,
		},
		{
			Command:      "id",
			Description:  "获取telegram信息",
			Handler:      idHandler,
			RequireAdmin: false,
		},
		{
			Command:      "help",
			Description:  "获取帮助信息",
			Handler:      helpHandler,
			RequireAdmin: true,
		},
		{
			Command:      "get_admins",
			Description:  "获取管理员信息",
			Handler:      getAminHandler,
			RequireAdmin: true,
		},
		{
			Command:      "upload_domains",
			Description:  "批量导入域名信息（命令行形式）",
			Handler:      UploadDomainsHandler,
			RequireAdmin: true,
		},
		{
			Command:      "export",
			Description:  "导出域名数据",
			Handler:      ExportDomainsHandler,
			RequireAdmin: true,
		},
		{
			Command:      "list_domains",
			Description:  "列出所有已配置的主域名",
			Handler:      listDomainsHandler,
			RequireAdmin: true,
		},
		{
			Command:      "list_admins",
			Description:  "列出管理员并进行管理",
			Handler:      listAdminsHandler,
			RequireAdmin: true,
		},
		{
			Command:      "manual_check",
			Description:  "手动执行一次完整的域名检测和自动切换",
			Handler:      manualCheckHandler,
			RequireAdmin: true,
		},
	}
}
