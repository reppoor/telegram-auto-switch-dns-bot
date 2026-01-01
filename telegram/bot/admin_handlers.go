package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-auto-switch-dns-bot/CheckBackend"
	"telegram-auto-switch-dns-bot/cloudflare"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/db"

	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/db/operate"
	"telegram-auto-switch-dns-bot/middleware"
	"telegram-auto-switch-dns-bot/utils"
)

// AdminRemarkSession 会话：设置管理员备注
type AdminRemarkSession struct {
	TargetUID int64
	ChatID    int64
	MessageID int
}

var adminRemarkSessions = make(map[int64]AdminRemarkSession) // 操作人 -> 会话信息

// DomainEditSession 域名/转发编辑会话
type DomainEditSession struct {
	DomainID  uint
	Field     string // name, port, sort
	ChatID    int64
	MessageID int
}

type ForwardEditSession struct {
	ForwardID uint
	Field     string // domain, ip, isp, weight, sort, type
	ChatID    int64
	MessageID int
}

var domainEditSessions = make(map[int64]DomainEditSession)
var forwardEditSessions = make(map[int64]ForwardEditSession)

// 列出管理员（带按钮）- 仅超管
func listAdminsHandler(ctx UpdateContext) {
	// 检查是否为超管
	if !middleware.CanManageAdmins(ctx.UserID) {
		SendMessage(ctx, 0, false, "⛔ 权限不足：仅超级管理员可以管理管理员列表")
		return
	}

	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			SendMessage(ctx, 0, false, "数据库初始化失败：%v", err)
			return
		}
	}

	var admins []models.TelegramAdmins
	if err := db.DB.Order("id asc").Find(&admins).Error; err != nil {
		SendMessage(ctx, 0, false, "获取管理员列表失败：%v", err)
		return
	}
	if len(admins) == 0 {
		SendMessage(ctx, 0, false, "暂无管理员记录")
		return
	}

	kb := AdminsKeyboard(admins)
	msg := tgbotapi.NewMessage(ctx.Update.Message.Chat.ID, "请选择一个管理员进行管理：")
	msg.ReplyMarkup = kb
	_, _ = ctx.Bot.Send(msg)
}

// 列出管理员（内联编辑）- 仅超管
func showAdminListInline(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("数据库初始化失败：%v", err))
			_, _ = bot.Send(edit)
			return
		}
	}

	var admins []models.TelegramAdmins
	if err := db.DB.Order("id asc").Find(&admins).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("获取管理员列表失败：%v", err))
		_, _ = bot.Send(edit)
		return
	}
	if len(admins) == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "暂无管理员记录")
		_, _ = bot.Send(edit)
		return
	}

	kb := AdminsKeyboard(admins)
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, "请选择一个管理员进行管理：", kb)
	_, _ = bot.Send(edit)
}

// 展示主域名详情（编辑当前消息）
func showDomainDetail(bot *tgbotapi.BotAPI, chatID int64, messageID int, domainID uint) {
	// 确保 DB 初始化
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("数据库初始化失败：%v", err))
			_, _ = bot.Send(edit)
			return
		}
	}

	var d models.DomainRecord
	if err := db.DB.Where("id = ?", domainID).First(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该主域名")
		_, _ = bot.Send(edit)
		return
	}

	status := "✅ 启用检测"
	if d.IsDisableCheck {
		status = "🚫 禁用检测"
	}

	dnsIDText := d.RecordId
	if dnsIDText == "" {
		dnsIDText = "未设置"
	}

	zoneIDText := d.ZoneId
	if zoneIDText == "" {
		zoneIDText = "未设置"
	}

	text := fmt.Sprintf(
		"🏛 *主域名详情*\n\n"+
			"*ID*: `%d`\n"+
			"*域名*: `%s`\n"+
			"*端口*: `%d`\n"+
			"*排序*: `%d`\n"+
			"*检测状态*: `%s`\n"+
			"*DNS ID*: `%s`\n"+
			"*Zone ID*: `%s`",
		d.ID, d.Domain, d.Port, d.SortOrder, status, dnsIDText, zoneIDText,
	)

	edit := tgbotapi.NewEditMessageTextAndMarkup(
		chatID,
		messageID,
		text,
		DomainActionsKeyboard(d),
	)
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}

// 开始设置备注（编辑当前消息）
func beginAdminRemark(userID int64, targetUID int64, bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	adminRemarkSessions[userID] = AdminRemarkSession{
		TargetUID: targetUID,
		ChatID:    chatID,
		MessageID: messageID,
	}
	edit := tgbotapi.NewEditMessageText(chatID, messageID, "📝 *设置备注*\n\n请输入备注内容：")
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}

// 处理备注输入
func handleAdminRemarkInput(ctx UpdateContext) bool {
	session, ok := adminRemarkSessions[ctx.UserID]
	if !ok {
		return false
	}
	remark := strings.TrimSpace(ctx.Update.Message.Text)
	var a models.TelegramAdmins
	_ = db.DB.Where("uid = ?", session.TargetUID).First(&a).Error
	if a.UID == 0 {
		edit := tgbotapi.NewEditMessageText(session.ChatID, session.MessageID, "未找到该管理员")
		_, _ = ctx.Bot.Send(edit)
		delete(adminRemarkSessions, ctx.UserID)
		return true
	}
	a.Remark = remark
	if err := operate.UpdateAdministrator(db.DB, a); err != nil {
		edit := tgbotapi.NewEditMessageText(session.ChatID, session.MessageID, fmt.Sprintf("更新备注失败：%v", err))
		_, _ = ctx.Bot.Send(edit)
		delete(adminRemarkSessions, ctx.UserID)
		return true
	}

	// 显示成功消息
	edit := tgbotapi.NewEditMessageText(session.ChatID, session.MessageID, "✅ *备注更新成功*\n\n新备注："+remark)
	edit.ParseMode = "Markdown"
	_, _ = ctx.Bot.Send(edit)

	// 2秒后返回管理员详情页
	time.Sleep(2 * time.Second)
	showAdminDetailInline(ctx.Bot, session.ChatID, session.MessageID, session.TargetUID)

	delete(adminRemarkSessions, ctx.UserID)
	return true
}

// 切换主域名检测开关（静默更新，不弹出消息）
func handleDomainToggleCheck(domainID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			utils.Logger.Errorf("数据库初始化失败：%v", err)
			return
		}
	}

	var d models.DomainRecord
	if err := db.DB.Where("id = ?", domainID).First(&d).Error; err != nil {
		utils.Logger.Errorf("未找到主域名 ID=%d: %v", domainID, err)
		return
	}

	// 切换检测状态
	d.IsDisableCheck = !d.IsDisableCheck
	if err := operate.UpdateDomainRecord(db.DB, d); err != nil {
		utils.Logger.Errorf("更新检测状态失败：%v", err)
		return
	}

	statusText := "启用检测"
	if d.IsDisableCheck {
		statusText = "禁用检测"
	}
	utils.Logger.Infof("✅ 主域名 %s (ID=%d) 检测状态已切换为: %s", d.Domain, domainID, statusText)
}

// 显示封禁确认界面（编辑当前消息）
func showAdminBanConfirm(bot *tgbotapi.BotAPI, chatID int64, messageID int, uid int64) {
	var a models.TelegramAdmins
	if db.DB != nil {
		_ = db.DB.Where("uid = ?", uid).First(&a).Error
	}
	if a.UID == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该管理员")
		_, _ = bot.Send(edit)
		return
	}

	var text string
	if a.IsBan {
		// 当前已封禁，询问是否解封
		// 转义用户输入的特殊字符
		firstNameEscaped := escapeMarkdownV2(a.FirstName)
		lastNameEscaped := escapeMarkdownV2(a.LastName)
		usernameEscaped := escapeMarkdownV2(a.Username)
		text = fmt.Sprintf(
			"⚠️ *确认解除封禁*\n\n"+
				"*UID*: `%d`\n"+
				"*姓名*: %s %s\n"+
				"*用户名*: @%s\n\n"+
				"确定要解除封禁此管理员吗？",
			a.UID, firstNameEscaped, lastNameEscaped, usernameEscaped)
	} else {
		// 当前未封禁，询问是否封禁
		firstNameEscaped := escapeMarkdownV2(a.FirstName)
		lastNameEscaped := escapeMarkdownV2(a.LastName)
		usernameEscaped := escapeMarkdownV2(a.Username)
		text = fmt.Sprintf(
			"⚠️ *确认封禁*\n\n"+
				"*UID*: `%d`\n"+
				"*姓名*: %s %s\n"+
				"*用户名*: @%s\n\n"+
				"⚠️ 封禁后该管理员将无法使用任何管理命令，确定要封禁吗？",
			a.UID, firstNameEscaped, lastNameEscaped, usernameEscaped)
	}

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, AdminBanConfirmKeyboard(uid, a.IsBan))
	edit.ParseMode = "MarkdownV2"
	_, _ = bot.Send(edit)
}

// 显示删除管理员确认界面（编辑当前消息）
func showAdminDeleteConfirm(bot *tgbotapi.BotAPI, chatID int64, messageID int, uid int64) {
	var a models.TelegramAdmins
	if db.DB != nil {
		_ = db.DB.Where("uid = ?", uid).First(&a).Error
	}
	if a.UID == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该管理员")
		_, _ = bot.Send(edit)
		return
	}

	// 转义用户输入的特殊字符
	firstNameEscaped := escapeMarkdownV2(a.FirstName)
	lastNameEscaped := escapeMarkdownV2(a.LastName)
	usernameEscaped := escapeMarkdownV2(a.Username)
	text := fmt.Sprintf(
		"⚠️ *确认删除管理员*\n\n"+
			"*UID*: `%d`\n"+
			"*姓名*: %s %s\n"+
			"*用户名*: @%s\n\n"+
			"⚠️ 此操作不可撤销，确定要删除此管理员吗？",
		a.UID, firstNameEscaped, lastNameEscaped, usernameEscaped)

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, AdminDeleteConfirmKeyboard(uid))
	edit.ParseMode = "MarkdownV2"
	_, _ = bot.Send(edit)
}

// 处理管理员删除
func handleAdminDelete(bot *tgbotapi.BotAPI, chatID int64, messageID int, uid int64) {
	var a models.TelegramAdmins
	if db.DB != nil {
		_ = db.DB.Where("uid = ?", uid).First(&a).Error
	}
	if a.UID == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该管理员")
		_, _ = bot.Send(edit)
		return
	}

	// 删除管理员
	if err := db.DB.Where("uid = ?", uid).Delete(&models.TelegramAdmins{}).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "删除管理员失败："+err.Error())
		_, _ = bot.Send(edit)
		return
	}

	// 显示成功消息并返回管理员列表
	edit := tgbotapi.NewEditMessageText(chatID, messageID, "✅ *管理员删除成功*\n\n已删除管理员: `"+strconv.FormatInt(uid, 10)+"`")
	edit.ParseMode = "MarkdownV2"
	_, _ = bot.Send(edit)

	// 2秒后自动返回到管理员列表
	time.Sleep(2 * time.Second)
	showAdminListInline(bot, chatID, messageID)
}

// 切换封禁/解封（编辑当前消息）
func handleAdminBanToggle(bot *tgbotapi.BotAPI, chatID int64, messageID int, uid int64, unban bool) {
	var a models.TelegramAdmins
	if db.DB != nil {
		_ = db.DB.Where("uid = ?", uid).First(&a).Error
	}
	if a.UID == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该管理员")
		_, _ = bot.Send(edit)
		return
	}
	a.IsBan = !unban
	if err := operate.UpdateAdministrator(db.DB, a); err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "更新封禁状态失败："+err.Error())
		_, _ = bot.Send(edit)
		return
	}

	// 显示成功消息并返回详情页
	var statusText string
	if a.IsBan {
		statusText = "✅ *封禁成功*\n\n该管理员已被封禁，无法使用管理命令。"
	} else {
		statusText = "✅ *解除封禁成功*\n\n该管理员已解除封禁，可以正常使用管理命令。"
	}
	edit := tgbotapi.NewEditMessageText(chatID, messageID, statusText)
	edit.ParseMode = "MarkdownV2"
	_, _ = bot.Send(edit)

	// 2秒后自动返回到管理员详情页
	time.Sleep(2 * time.Second)
	showAdminDetailInline(bot, chatID, messageID, uid)
}

// 展示管理员详情（编辑当前消息）
func showAdminDetailInline(bot *tgbotapi.BotAPI, chatID int64, messageID int, uid int64) {
	utils.Logger.Infof("[DEBUG] showAdminDetailInline 被调用: uid=%d, chatID=%d, messageID=%d", uid, chatID, messageID)
	var a models.TelegramAdmins
	if db.DB != nil {
		utils.Logger.Infof("[DEBUG] 尝试从数据库查询 UID=%d", uid)
		_ = db.DB.Where("uid = ?", uid).First(&a).Error
		if a.UID != 0 {
			utils.Logger.Infof("[DEBUG] 在数据库中找到: %s %s (UID: %d)", a.FirstName, a.LastName, a.UID)
		}
	}
	if a.UID == 0 {
		utils.Logger.Errorf("[DEBUG] 未找到该管理员: UID=%d", uid)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该管理员")
		_, _ = bot.Send(edit)
		return
	}

	utils.Logger.Infof("[DEBUG] 找到管理员，准备生成详情消息: UID=%d, Name=%s %s", a.UID, a.FirstName, a.LastName)

	banStatus := "✅ 正常"
	if a.IsBan {
		banStatus = "🚫 已封禁"
	}

	name := a.FirstName
	if a.LastName != "" {
		name += " " + a.LastName
	}
	if name == "" {
		name = "未设置"
	}

	username := a.Username
	if username == "" {
		username = "未设置"
	}

	remark := a.Remark
	if remark == "" {
		remark = "无"
	}

	utils.Logger.Infof("[DEBUG] 生成消息文本，UID=%d, username=%s, name=%s", a.UID, username, name)

	// 转义 Markdown 特殊字符
	usernameEscaped := escapeMarkdown(username)
	nameEscaped := escapeMarkdown(name)
	remarkEscaped := escapeMarkdown(remark)
	roleEscaped := escapeMarkdown(a.Role)

	text := fmt.Sprintf(
		"👤 *管理员详情*\n\n"+
			"*UID*: `%d`\n"+
			"*用户名*: %s\n"+
			"*姓名*: %s\n"+
			"*角色*: %s\n"+
			"*封禁状态*: %s\n"+
			"*备注*: %s",
		a.UID, usernameEscaped, nameEscaped, roleEscaped, banStatus, remarkEscaped)

	utils.Logger.Infof("[DEBUG] 准备编辑消息，chatID=%d, messageID=%d", chatID, messageID)
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, AdminActionsKeyboard(a))
	edit.ParseMode = "Markdown"

	utils.Logger.Infof("[DEBUG] 发送编辑消息请求...")
	resp, err := bot.Send(edit)
	if err != nil {
		utils.Logger.Errorf("[DEBUG] ❌ 发送消息失败: %v", err)
	} else {
		utils.Logger.Infof("[DEBUG] ✅ 消息发送成功: %+v", resp)
	}
}

// 切换转发封禁状态
func handleForwardToggleBan(bot *tgbotapi.BotAPI, chatID int64, forwardID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			msg := tgbotapi.NewMessage(chatID, "数据库初始化失败："+err.Error())
			_, _ = bot.Send(msg)
			return
		}
	}

	var f models.ForwardRecord
	if err := db.DB.Where("id = ?", forwardID).First(&f).Error; err != nil {
		msg := tgbotapi.NewMessage(chatID, "未找到该转发记录")
		_, _ = bot.Send(msg)
		return
	}

	f.IsBan = !f.IsBan
	// 如果封禁，设置封禁时间为一年后；如果解封，清零封禁时间
	if f.IsBan {
		f.BanTime = time.Now().AddDate(1, 0, 0).Unix() // 一年后
	} else {
		f.BanTime = 0 // 初始值
	}

	if err := operate.UpdateForwardRecord(db.DB, f); err != nil {
		msg := tgbotapi.NewMessage(chatID, "更新封禁状态失败："+err.Error())
		_, _ = bot.Send(msg)
		return
	}

	// 已切换封禁状态,界面更新由回调处理进行编辑
}

// 显示转发删除确认界面
func showForwardDeleteConfirm(bot *tgbotapi.BotAPI, chatID int64, messageID int, forwardID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			msg := tgbotapi.NewMessage(chatID, "数据库初始化失败："+err.Error())
			_, _ = bot.Send(msg)
			return
		}
	}

	var f models.ForwardRecord
	if err := db.DB.Where("id = ?", forwardID).First(&f).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该转发记录")
		_, _ = bot.Send(edit)
		return
	}

	idStr := strconv.FormatUint(uint64(forwardID), 10)
	text := fmt.Sprintf(
		"⚠️ *确认删除*\n\n"+
			"*转发域名*: `%s`\n"+
			"*IP*: `%s`\n"+
			"*ISP*: `%s`\n\n"+
			"⚠️ 此操作不可撤销，确定要删除吗？",
		f.ForwardDomain, f.IP, f.ISP,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ 确认删除", "fwd_delete_confirm:"+idStr),
			tgbotapi.NewInlineKeyboardButtonData("❌ 取消", "fwd_delete_cancel:"+idStr),
		),
	)

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, keyboard)
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}

// 处理转发删除
func handleForwardDelete(bot *tgbotapi.BotAPI, chatID int64, messageID int, forwardID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			msg := tgbotapi.NewMessage(chatID, "数据库初始化失败："+err.Error())
			_, _ = bot.Send(msg)
			return
		}
	}

	var f models.ForwardRecord
	if err := db.DB.Where("id = ?", forwardID).First(&f).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 未找到该转发记录")
		_, _ = bot.Send(edit)
		return
	}

	domainID := f.DomainRecordID
	forwardName := f.ForwardDomain

	// 删除数据库记录
	if err := db.DB.Delete(&f).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 删除失败："+err.Error())
		_, _ = bot.Send(edit)
		return
	}

	text := fmt.Sprintf("✅ *删除成功*\n\n已删除转发域名: `%s`", forwardName)
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)

	// 2秒后自动返回到转发列表
	time.Sleep(2 * time.Second)
	editForwards(bot, chatID, messageID, domainID)
}

// 处理获取转发域名 IP（调用后端 check_api.go 接口）
func handleForwardGetIP(bot *tgbotapi.BotAPI, chatID int64, messageID int, forwardID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 数据库初始化失败")
			_, _ = bot.Send(edit)
			return
		}
	}

	// 获取转发记录
	var f models.ForwardRecord
	if err := db.DB.Where("id = ?", forwardID).First(&f).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 未找到该转发记录")
		_, _ = bot.Send(edit)
		return
	}

	// 获取主域名信息（需要 port）
	var d models.DomainRecord
	if err := db.DB.Where("id = ?", f.DomainRecordID).First(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 未找到对应的主域名")
		_, _ = bot.Send(edit)
		return
	}

	// 显示获取中状态
	edit := tgbotapi.NewEditMessageText(chatID, messageID, "🔍 *正在获取 IP...*\n\n请稍候...")
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)

	// 调用后端 resolve_ip 接口获取 IP（不进行连通性检测）
	targetIP, err := callBackendResolveIP(f.ForwardDomain, d.Port)
	if err != nil || targetIP == "" {
		// 失败则写入 0.0.0.0
		targetIP = "0.0.0.0"
		utils.Logger.Warnf("❌ 获取 %s 的 IP 失败: %v，写入 0.0.0.0", f.ForwardDomain, err)
	} else {
		utils.Logger.Infof("✅ 成功获取 %s 的 IP: %s", f.ForwardDomain, targetIP)
	}

	// 更新数据库
	f.IP = targetIP
	if err := operate.UpdateForwardRecord(db.DB, f); err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID,
			fmt.Sprintf("❌ 更新数据库失败：%v", err))
		_, _ = bot.Send(edit)
		time.Sleep(2 * time.Second)
		editForwardInfo(bot, chatID, messageID, forwardID)
		return
	}

	// 显示结果消息
	var resultMsg string
	if targetIP == "0.0.0.0" {
		resultMsg = fmt.Sprintf(
			"⚠️ *获取 IP 失败*\n\n"+
				"转发域名: `%s`\n"+
				"已写入: `%s`\n\n"+
				"原因: 无法解析域名",
			f.ForwardDomain, targetIP,
		)
	} else {
		resultMsg = fmt.Sprintf(
			"✅ *获取 IP 成功*\n\n"+
				"转发域名: `%s`\n"+
				"IP 地址: `%s`",
			f.ForwardDomain, targetIP,
		)
	}

	edit = tgbotapi.NewEditMessageText(chatID, messageID, resultMsg)
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)

	time.Sleep(2 * time.Second)
	editForwardInfo(bot, chatID, messageID, forwardID)
}

// 调用后端 resolve_ip 接口获取 IP（POST /api/v1/resolve_ip）
func callBackendResolveIP(target string, port int) (string, error) {
	// 构建请求 URL
	url := fmt.Sprintf("%s/api/v1/resolve_ip", config.Global.BackendURL.Api)

	// 构建请求体
	payload := CheckBackend.TCPCheckRequest{
		Target: target,
		Port:   port,
		Key:    config.Global.BackendListen.Key,
	}
	buf, _ := json.Marshal(payload)

	// 发送 POST 请求
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用后端接口失败: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	var apiResp CheckBackend.APIResponse[CheckBackend.TCPCheckResponse]
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if apiResp.Code != 0 {
		return "", fmt.Errorf("后端返回错误: %s", apiResp.Message)
	}

	// 返回获取到的 IP
	if apiResp.Data.TargetIp != "" {
		return apiResp.Data.TargetIp, nil
	}

	// 如果没有获取到 IP
	return "", fmt.Errorf("无法解析域名: %s", apiResp.Data.Message)
}

// 显示主域名删除确认界面
func showDomainDeleteConfirm(bot *tgbotapi.BotAPI, chatID int64, messageID int, domainID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			msg := tgbotapi.NewMessage(chatID, "数据库初始化失败："+err.Error())
			_, _ = bot.Send(msg)
			return
		}
	}

	var d models.DomainRecord
	if err := db.DB.Preload("Forwards").Where("id = ?", domainID).First(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该主域名")
		_, _ = bot.Send(edit)
		return
	}

	idStr := strconv.FormatUint(uint64(domainID), 10)
	forwardCount := len(d.Forwards)
	text := fmt.Sprintf(
		"⚠️ *确认删除*\n\n"+
			"*主域名*: `%s`\n"+
			"*端口*: `%d`\n"+
			"*转发数量*: `%d`\n\n"+
			"⚠️ 此操作将同时删除该主域名下所有转发记录，不可撤销，确定要删除吗？",
		d.Domain, d.Port, forwardCount,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ 确认删除", "dom_delete_confirm:"+idStr),
			tgbotapi.NewInlineKeyboardButtonData("❌ 取消", "dom_delete_cancel:"+idStr),
		),
	)

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, keyboard)
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}

// 处理主域名删除
func handleDomainDelete(bot *tgbotapi.BotAPI, chatID int64, messageID int, domainID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			msg := tgbotapi.NewMessage(chatID, "数据库初始化失败："+err.Error())
			_, _ = bot.Send(msg)
			return
		}
	}

	var d models.DomainRecord
	if err := db.DB.Preload("Forwards").Where("id = ?", domainID).First(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 未找到该主域名")
		_, _ = bot.Send(edit)
		return
	}

	domainName := d.Domain
	// 查询该主域名下的所有转发记录（避免依赖 Preload 结果）
	var forwards []models.ForwardRecord
	_ = db.DB.Where("domain_record_id = ?", domainID).Find(&forwards).Error
	forwardCount := len(forwards)

	// 1️⃣ 先删除所有转发记录的关联数据（使用查询结果，避免 Preload 空列表）
	for _, f := range forwards {
		// 由于已弃用缓存，跳过Redis缓存删除操作
		utils.Logger.Debugf("跳过转发记录缓存删除: forward:%d", f.ID)
	}

	// 2️⃣ 显式删除所有转发记录（数据库，始终执行）
	if err := db.DB.Where("domain_record_id = ?", domainID).Delete(&models.ForwardRecord{}).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 删除转发记录失败："+err.Error())
		_, _ = bot.Send(edit)
		return
	}
	utils.Logger.Infof("✅ 已删除主域名 ID=%d 的所有转发记录，共 %d 条", domainID, forwardCount)

	// 3️⃣ 删除主域名记录（数据库）
	if err := db.DB.Delete(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 删除主域名失败："+err.Error())
		_, _ = bot.Send(edit)
		return
	}
	utils.Logger.Infof("✅ 已删除主域名: %s (ID=%d)", domainName, domainID)

	text := fmt.Sprintf("✅ *删除成功*\n\n已删除主域名: `%s` 及其 `%d` 个转发记录", domainName, forwardCount)
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)

	// 2秒后自动返回到主域名列表
	time.Sleep(2 * time.Second)
	editDomainList(bot, chatID, messageID)
}

// 处理主域名编辑输入
func handleDomainEditInput(ctx UpdateContext) bool {
	session, ok := domainEditSessions[ctx.UserID]
	if !ok {
		return false
	}

	text := strings.TrimSpace(ctx.Update.Message.Text)
	if text == "" {
		SendMessage(ctx, 0, false, "❌ 输入不能为空，请重新输入。")
		return true
	}

	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			SendMessage(ctx, 0, false, "❌ 数据库初始化失败：%v", err)
			return true
		}
	}

	var d models.DomainRecord
	if err := db.DB.Where("id = ?", session.DomainID).First(&d).Error; err != nil {
		SendMessage(ctx, 0, false, "❌ 未找到该主域名")
		delete(domainEditSessions, ctx.UserID)
		return true
	}

	oldDomainName := d.Domain
	// 这里不再保存旧值/新值（已简化提示逻辑）

	switch session.Field {
	case "name":
		d.Domain = text
	case "port":
		port, err := strconv.Atoi(text)
		if err != nil {
			SendMessage(ctx, 0, false, "❌ 端口必须是数字，请重新输入。")
			return true
		}
		d.Port = port
	case "sort":
		sortVal, err := strconv.Atoi(text)
		if err != nil {
			SendMessage(ctx, 0, false, "❌ 排序值必须是数字，请重新输入。")
			return true
		}
		d.SortOrder = sortVal
	default:
		SendMessage(ctx, 0, false, "❌ 未知字段，编辑失败。")
		delete(domainEditSessions, ctx.UserID)
		return true
	}

	// 如果修改了域名，需要验证新的域名在 Cloudflare 中是否存在对应的 RecordId 和 ZoneId
	if session.Field == "name" {
		// 提取根域名
		rootDomain := extractRootDomain(text)
		utils.Logger.Infof("📌 域名: %s, 根域名: %s", text, rootDomain)

		// 使用根域名创建 Cloudflare 客户端
		cfClient, err := cloudflare.NewClientByDomain(rootDomain)
		if err != nil {
			SendMessage(ctx, 0, false, "❌ 无法连接 Cloudflare (根域名: %s): %v", rootDomain, err)
			// 返回详情页（主域名）
			showDomainDetail(ctx.Bot, session.ChatID, session.MessageID, d.ID)
			delete(domainEditSessions, ctx.UserID)
			return true
		}

		// 获取 Zone ID
		zoneID, err := cloudflare.GetZoneIDByDomain(config.Global.Cloudflare.ApiToken, rootDomain)
		if err != nil {
			SendMessage(ctx, 0, false, "❌ 无法获取域名 %s 的 Zone ID: %v", rootDomain, err)
			// 返回详情页（主域名）
			showDomainDetail(ctx.Bot, session.ChatID, session.MessageID, d.ID)
			delete(domainEditSessions, ctx.UserID)
			return true
		}

		// 使用完整域名查找 DNS 记录并获取 ID
		ctxBg := context.Background()
		dnsRecord, err := cfClient.GetDNSRecordByName(ctxBg, text, "")
		if err != nil {
			SendMessage(ctx, 0, false, "❌ 域名 %s 在 Cloudflare 中不存在对应的 DNS 记录，请先在 Cloudflare 中创建该域名的 DNS 记录: %v", text, err)
			// 返回详情页（主域名）
			showDomainDetail(ctx.Bot, session.ChatID, session.MessageID, d.ID)
			delete(domainEditSessions, ctx.UserID)
			return true
		}

		// 设置新的 RecordId 和 ZoneId
		d.RecordId = dnsRecord.ID
		d.ZoneId = zoneID
		utils.Logger.Infof("✅ 自动获取 DNS ID：%s -> %s (类型: %s, 内容: %s)", text, dnsRecord.ID, dnsRecord.Type, dnsRecord.Content)
		utils.Logger.Infof("✅ 自动获取 Zone ID：%s -> %s", text, zoneID)
	}

	if err := operate.UpdateDomainRecord(db.DB, d); err != nil {
		SendMessage(ctx, 0, false, "❌ 更新主域名失败：%v", err)
		// 返回详情页（主域名）
		showDomainDetail(ctx.Bot, session.ChatID, session.MessageID, d.ID)
		delete(domainEditSessions, ctx.UserID)
		return true
	}

	if session.Field == "name" && oldDomainName != d.Domain {
		utils.Logger.Infof("✅ 主域名已更新: %s -> %s", oldDomainName, d.Domain)
	}

	// 这里不再单独发成功消息，通过回到详情页 + 回调提示完成交互

	delete(domainEditSessions, ctx.UserID)

	showDomainDetail(ctx.Bot, session.ChatID, session.MessageID, d.ID)
	return true
}

// 处理转发记录编辑输入
func handleForwardEditInput(ctx UpdateContext) bool {
	session, ok := forwardEditSessions[ctx.UserID]
	if !ok {
		return false
	}

	text := strings.TrimSpace(ctx.Update.Message.Text)
	if text == "" {
		SendMessage(ctx, 0, false, "❌ 输入不能为空，请重新输入。")
		return true
	}

	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			SendMessage(ctx, 0, false, "❌ 数据库初始化失败：%v", err)
			return true
		}
	}

	// Check if we're adding a new forward record
	if session.Field == "add_forward" {
		return handleAddForwardInput(ctx, session, text)
	}

	var f models.ForwardRecord
	if err := db.DB.Where("id = ?", session.ForwardID).First(&f).Error; err != nil {
		SendMessage(ctx, 0, false, "❌ 未找到该转发记录")
		delete(forwardEditSessions, ctx.UserID)
		return true
	}

	// 这里不再保存旧值/新值（已简化提示逻辑）

	switch session.Field {
	case "domain":
		f.ForwardDomain = text
	case "ip":
		f.IP = text
	case "isp":
		f.ISP = text
	case "weight":
		w, err := strconv.Atoi(text)
		if err != nil {
			SendMessage(ctx, 0, false, "❌ 权重必须是数字，请重新输入。")
			return true
		}
		f.Weight = w
	case "sort":
		s, err := strconv.Atoi(text)
		if err != nil {
			SendMessage(ctx, 0, false, "❌ 排序值必须是数字，请重新输入。")
			return true
		}
		f.SortOrder = s
	case "type":
		f.RecordType = text
	default:
		SendMessage(ctx, 0, false, "❌ 未知字段，编辑失败。")
		delete(forwardEditSessions, ctx.UserID)
		return true
	}

	if err := operate.UpdateForwardRecord(db.DB, f); err != nil {
		SendMessage(ctx, 0, false, "❌ 更新转发记录失败：%v", err)
		// 返回转发详情页
		editForwardInfo(ctx.Bot, session.ChatID, session.MessageID, f.ID)
		delete(forwardEditSessions, ctx.UserID)
		return true
	}

	// 这里不再单独发成功消息，通过回到详情页 + 回调提示完成交互

	delete(forwardEditSessions, ctx.UserID)

	// 返回转发详情页
	editForwardInfo(ctx.Bot, session.ChatID, session.MessageID, f.ID)
	return true
}

// 处理转发检测并解析到 Cloudflare
func handleForwardCheckAndResolve(bot *tgbotapi.BotAPI, chatID int64, messageID int, forwardID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 数据库初始化失败")
			_, _ = bot.Send(edit)
			return
		}
	}

	// 获取转发记录和主域名信息
	var f models.ForwardRecord
	if err := db.DB.Where("id = ?", forwardID).First(&f).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 未找到该转发记录")
		_, _ = bot.Send(edit)
		return
	}

	var d models.DomainRecord
	if err := db.DB.Where("id = ?", f.DomainRecordID).First(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 未找到主域名信息")
		_, _ = bot.Send(edit)
		return
	}

	// 显示检测中状态
	edit := tgbotapi.NewEditMessageText(chatID, messageID, "🔍 *正在检测转发域名...*\n\n请稍候...")
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)

	// 调用 WebSocket 检测接口（带进度回调）
	checkResult, err := checkForwardDomainViaWSWithProgress(f.ForwardDomain, d.Port, func(progress string) {
		// 动态更新检测进度
		progressEdit := tgbotapi.NewEditMessageText(chatID, messageID,
			fmt.Sprintf("🔍 *正在检测转发域名...*\n\n%s", progress))
		progressEdit.ParseMode = "Markdown"
		_, _ = bot.Send(progressEdit)
	})
	if err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("❌ 检测失败：%v", err))
		_, _ = bot.Send(edit)
		time.Sleep(2 * time.Second)
		editForwardInfo(bot, chatID, messageID, forwardID)
		return
	}

	// 根据 RecordType 决定更新策略
	var targetIP string
	var resolveMsg string

	// 检查连通性，失败则不更新
	if !checkResult.Result {
		resolveMsg = fmt.Sprintf("❌ 域名不可访问\n%s", checkResult.Message)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("🔍 *检测结果*\n\n%s", resolveMsg))
		edit.ParseMode = "Markdown"
		_, _ = bot.Send(edit)
		time.Sleep(3 * time.Second)
		editForwardInfo(bot, chatID, messageID, forwardID)
		return
	}

	if f.RecordType == "A" {
		// A 记录：使用 TargetIp
		targetIP = checkResult.TargetIp
		resolveMsg = fmt.Sprintf("✅ 域名可访问\n解析 IP: `%s`", targetIP)

		// 更新转发域名的 IP 到数据库
		f.IP = targetIP
		if err := db.DB.Save(&f).Error; err != nil {
			utils.Logger.Warnf("更新转发域名 IP 失败: %v", err)
		} else {
			utils.Logger.Infof("✅ 已更新转发域名 %s 的 IP 为: %s", f.ForwardDomain, targetIP)

		}
	} else if f.RecordType == "CNAME" {
		// CNAME 记录：直接使用转发域名
		targetIP = f.ForwardDomain
		resolveMsg = fmt.Sprintf("✅ 域名可访问\nCNAME 目标: `%s`", targetIP)

		// 更新转发域名的 IP 到数据库（从检测结果获取）
		if checkResult.TargetIp != "" {
			f.IP = checkResult.TargetIp
			if err := db.DB.Save(&f).Error; err != nil {
				utils.Logger.Warnf("更新转发域名 IP 失败: %v", err)
			} else {
				utils.Logger.Infof("✅ 已更新转发域名 %s 的 IP 为: %s", f.ForwardDomain, checkResult.TargetIp)

			}
		} else {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ 不支持的记录类型")
			_, _ = bot.Send(edit)
			time.Sleep(2 * time.Second)
			editForwardInfo(bot, chatID, messageID, forwardID)
			return
		}

		// 显示检测结果并准备更新 Cloudflare
		edit = tgbotapi.NewEditMessageText(chatID, messageID,
			fmt.Sprintf("🔍 *检测完成*\n\n%s\n\n🔄 正在更新 Cloudflare DNS...", resolveMsg))
		edit.ParseMode = "Markdown"
		_, _ = bot.Send(edit)

		// 获取根域名并创建 Cloudflare 客户端
		rootDomain := extractRootDomain(d.Domain)
		cfClient, err := cloudflare.NewClientByDomain(rootDomain)
		if err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID,
				fmt.Sprintf("❌ Cloudflare 连接失败：%v", err))
			_, _ = bot.Send(edit)
			time.Sleep(2 * time.Second)
			editForwardInfo(bot, chatID, messageID, forwardID)
			return
		}

		// 使用主域名的 RecordId 更新 DNS 记录
		ctx := context.Background()
		var dnsErr error

		if f.RecordType == "A" {
			// 更新 A 记录
			_, dnsErr = cfClient.UpdateARecord(ctx, d.RecordId, d.Domain, targetIP, false)
		} else if f.RecordType == "CNAME" {
			// 更新 CNAME 记录
			_, dnsErr = cfClient.UpdateCNAMERecord(ctx, d.RecordId, d.Domain, targetIP, false)
		}

		if dnsErr != nil {
			// DNS 更新失败，记录失败状态
			f.ResolveStatus = "failed"
			f.LastResolvedAt = time.Now().Unix()
			_ = db.DB.Save(&f)

			edit := tgbotapi.NewEditMessageText(chatID, messageID,
				fmt.Sprintf("❌ DNS 更新失败：%v", dnsErr))
			_, _ = bot.Send(edit)
			time.Sleep(2 * time.Second)
			editForwardInfo(bot, chatID, messageID, forwardID)
			return
		}

		// DNS 更新成功，记录成功状态
		f.ResolveStatus = "success"
		f.LastResolvedAt = time.Now().Unix()

		// 清除同一主域名下其他转发域名的 success 状态
		if err := db.DB.Model(&models.ForwardRecord{}).Where(
			"domain_record_id = ? AND id != ?", f.DomainRecordID, f.ID,
		).Updates(map[string]interface{}{
			"resolve_status": "never",
		}).Error; err != nil {
			utils.Logger.Warnf("清除其他转发域名状态失败: %v", err)
		}

		if err := db.DB.Save(&f).Error; err != nil {
			utils.Logger.Warnf("更新解析状态失败: %v", err)
		} else {
			utils.Logger.Infof("✅ 已记录解析状态: %s", f.ForwardDomain)

		}

		// 更新成功
		successMsg := fmt.Sprintf(
			"✅ *检测并解析成功*\n\n"+
				"%s\n\n"+
				"🌐 已更新 Cloudflare DNS\n"+
				"*主域名*: `%s`\n"+
				"*记录类型*: `%s`\n"+
				"*目标值*: `%s`",
			resolveMsg, d.Domain, f.RecordType, targetIP,
		)
		edit = tgbotapi.NewEditMessageText(chatID, messageID, successMsg)
		edit.ParseMode = "Markdown"
		_, _ = bot.Send(edit)

		time.Sleep(3 * time.Second)
		editForwardInfo(bot, chatID, messageID, forwardID)
	}
}

// 提取根域名（取后两部分）
func extractRootDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return domain
}

// 通过 HTTP 检测转发域名（带进度回调）
func checkForwardDomainViaWSWithProgress(target string, port int, progressCallback func(string)) (*CheckBackend.TCPCheckResponse, error) {
	// 构建 HTTP 请求 URL
	// url := fmt.Sprintf("%s/api/v1/tcp_checks", config.Global.BackendURL.Api)

	if progressCallback != nil {
		progressCallback(fmt.Sprintf("📡 正在连接检测服务...\n目标: `%s:%d`", target, port))
	}

	// 发送请求并获取结果
	result, err := checkConnectivityWithProgress(target, port, func(current int, total int) {
		if progressCallback != nil {
			progressCallback(fmt.Sprintf("🔍 正在检测连通性...\n目标: `%s:%d`\n\n⚡ 第 %d/%d 次尝试连接...", target, port, current, total))
		}
	})
	if err != nil {
		return nil, fmt.Errorf("检测请求失败: %w", err)
	}

	// 转换结果格式
	response := &CheckBackend.TCPCheckResponse{
		Result:          result.Result,
		Target:          result.Target,
		TargetIp:        result.TargetIp,
		Message:         result.Message,
		BackendPublicIP: result.BackendPublicIP,
	}

	if progressCallback != nil {
		if response.Result {
			progressCallback(fmt.Sprintf("✅ 检测完成\n目标: `%s:%d`\n解析 IP: `%s`\n\n✨ 连接成功！", target, port, response.TargetIp))
		} else {
			progressCallback(fmt.Sprintf("❌ 检测完成\n目标: `%s:%d`\n\n⚠️ 连接失败", target, port))
		}
		time.Sleep(500 * time.Millisecond) // 稍微延迟让用户看到最后状态
	}

	return response, nil
}

// handleAddForwardInput handles the input for adding a new forward record
func handleAddForwardInput(ctx UpdateContext, session ForwardEditSession, text string) bool {
	// Parse the input: "转发域名|IP|ISP|权重|排序|记录类型"
	parts := strings.Split(text, "|")
	if len(parts) < 6 {
		SendMessage(ctx, 0, false, "❌ 输入格式错误，请按照格式输入：转发域名|IP|ISP|权重|排序|记录类型")
		// Show the form again
		showAddForwardForm(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID, ctx.UserID)
		return true
	}

	// Extract values
	forwardDomain := strings.TrimSpace(parts[0])
	ip := strings.TrimSpace(parts[1])
	isp := strings.TrimSpace(parts[2])

	// Parse weight
	weight, err := strconv.Atoi(strings.TrimSpace(parts[3]))
	if err != nil {
		SendMessage(ctx, 0, false, "❌ 权重必须是数字，请重新输入。")
		showAddForwardForm(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID, ctx.UserID)
		return true
	}

	// Parse sort order
	sortOrder, err := strconv.Atoi(strings.TrimSpace(parts[4]))
	if err != nil {
		SendMessage(ctx, 0, false, "❌ 排序值必须是数字，请重新输入。")
		showAddForwardForm(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID, ctx.UserID)
		return true
	}

	// Get record type
	recordType := strings.TrimSpace(parts[5])
	if recordType == "" {
		recordType = "A" // Default to A record
	}

	// Validate forward domain
	if forwardDomain == "" {
		SendMessage(ctx, 0, false, "❌ 转发域名不能为空，请重新输入。")
		showAddForwardForm(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID, ctx.UserID)
		return true
	}

	// Check if the forward domain already exists for this domain ID
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			SendMessage(ctx, 0, false, "❌ 数据库初始化失败：%v", err)
			delete(forwardEditSessions, ctx.UserID)
			editForwards(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID)
			return true
		}
	}

	var existingForward models.ForwardRecord
	if err := db.DB.Where("domain_record_id = ? AND forward_domain = ?", session.ForwardID, forwardDomain).First(&existingForward).Error; err == nil {
		// Forward domain already exists for this domain
		SendMessage(ctx, 0, false, "❌ 转发域名 `%s` 已存在于当前主域名下，请勿重复添加。", forwardDomain)
		delete(forwardEditSessions, ctx.UserID)
		editForwards(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID)
		return true
	}

	// Create the new forward record
	forward := models.ForwardRecord{
		DomainRecordID: session.ForwardID, // This is actually the domain ID
		ForwardDomain:  forwardDomain,
		IP:             ip,
		ISP:            isp,
		Weight:         weight,
		SortOrder:      sortOrder,
		RecordType:     recordType,
		ResolveStatus:  "never", // Default status
	}

	// Add the forward record to the database
	if err := operate.AddForwardRecord(db.DB, forward); err != nil {
		SendMessage(ctx, 0, false, "❌ 添加转发记录失败：%v", err)
		delete(forwardEditSessions, ctx.UserID)
		// Return to the forwards list
		editForwards(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID)
		return true
	}

	// Success message
	SendMessage(ctx, 0, false, "✅ 转发记录添加成功！")

	// Clean up session
	delete(forwardEditSessions, ctx.UserID)

	// Return to the forwards list
	editForwards(ctx.Bot, session.ChatID, session.MessageID, session.ForwardID)
	return true
}

// showAddForwardForm displays the form for adding a new forward record
func showAddForwardForm(bot *tgbotapi.BotAPI, chatID int64, messageID int, domainID uint, userID int64) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID,
		"➕ *添加转发域名*\n\n"+
			"请按照以下格式输入转发域名信息：\n"+
			"`转发域名|IP|ISP|权重|排序|记录类型`\n\n"+
			"*示例*:\n"+
			"`cdn1.example.com|1.1.1.1|电信|10|1|A`\n"+
			"`cdn2.example.com||联通|20|2|CNAME`\n\n"+
			"*说明*:\n"+
			"- IP 可为空，系统会自动解析\n"+
			"- ISP 可为空\n"+
			"- 权重数值越大优先级越高\n"+
			"- 记录类型可选 A 或 CNAME，默认为 A\n\n"+
			"请输入：")
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)

	// Store session for handling the input
	forwardEditSessions[userID] = ForwardEditSession{
		ForwardID: domainID, // Store the domain ID for new forwards
		Field:     "add_forward",
		ChatID:    chatID,
		MessageID: messageID,
	}
}
