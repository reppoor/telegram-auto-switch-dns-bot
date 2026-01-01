package bot

import (
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-auto-switch-dns-bot/db"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/db/operate"
	"telegram-auto-switch-dns-bot/middleware"
	"telegram-auto-switch-dns-bot/utils"
)

func HandleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	var userID int64
	// 回调查询处理（inline keyboard）
	if update.CallbackQuery != nil {
		userID = update.CallbackQuery.From.ID
		data := update.CallbackQuery.Data

		// 权限校验：回调也必须是管理员
		if !middleware.IsSuperAdmin(userID) {
			if db.DB == nil {
				_ = db.InitDB()
			}
			isAdmin, isBanned, err := middleware.IsAdminAndNotBanned(userID)
			if err != nil || !isAdmin {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 权限不足：需要管理员权限"))
				return
			}
			if isBanned {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 您的账号已被封禁"))
				return
			}
		}

		if strings.HasPrefix(data, "dom:") {
			idStr := strings.TrimPrefix(data, "dom:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showDomainDetail(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "dom_forwards:") {
			idStr := strings.TrimPrefix(data, "dom_forwards:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			editForwards(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "dom_toggle_check:") {
			idStr := strings.TrimPrefix(data, "dom_toggle_check:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleDomainToggleCheck(uint(did))
			showDomainDetail(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "dom_delete:") {
			idStr := strings.TrimPrefix(data, "dom_delete:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showDomainDeleteConfirm(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "dom_delete_confirm:") {
			idStr := strings.TrimPrefix(data, "dom_delete_confirm:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleDomainDelete(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "dom_delete_cancel:") {
			idStr := strings.TrimPrefix(data, "dom_delete_cancel:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showDomainDetail(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "dom_edit:") {
			parts := strings.Split(data, ":")
			if len(parts) >= 3 {
				did, _ := strconv.ParseUint(parts[1], 10, 64)
				field := parts[2]
				chatID := update.CallbackQuery.Message.Chat.ID
				msgID := update.CallbackQuery.Message.MessageID
				domainEditSessions[userID] = DomainEditSession{
					DomainID:  uint(did),
					Field:     field,
					ChatID:    chatID,
					MessageID: msgID,
				}
				text := "✏️ 请输入新的值："
				switch field {
				case "name":
					text = "✏️ *修改域名*\n\n请输入新的主域名："
				case "port":
					text = "🔌 *修改端口*\n\n请输入新的端口（数字）："
				case "sort":
					text = "🔢 *修改排序*\n\n请输入新的排序值（数字）："
				}
				edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
				edit.ParseMode = "Markdown"
				_, _ = bot.Send(edit)
			}
			// 这里只负责进入编辑态，不提示成功，成功由文本消息提交后处理
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if data == "back:domains" {
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			editDomainList(bot, chatID, msgID)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if data == "back:admins" {
			// 返回管理员列表 - 仅超管
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showAdminListInline(bot, chatID, msgID)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "back:domain:") {
			idStr := strings.TrimPrefix(data, "back:domain:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showDomainDetail(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "back:forwards:") {
			idStr := strings.TrimPrefix(data, "back:forwards:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			editForwards(bot, chatID, msgID, uint(did))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm:") {
			// 仅超管可以查看管理员详情 - 使用内联编辑
			utils.Logger.Infof("[DEBUG] 收到管理员详情请求，callback data: %s, userID: %d", data, userID)
			if !middleware.CanManageAdmins(userID) {
				utils.Logger.Warnf("[DEBUG] 权限不足: userID %d 不是超管", userID)
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:"), 10, 64)
			utils.Logger.Infof("[DEBUG] 解析的 UID: %d", uid)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			utils.Logger.Infof("[DEBUG] 即将调用 showAdminDetailInline, chatID: %d, msgID: %d, uid: %d", chatID, msgID, uid)
			showAdminDetailInline(bot, chatID, msgID, uid)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_ban:") {
			// 仅超管可以封禁/解封 - 显示确认界面
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_ban:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showAdminBanConfirm(bot, chatID, msgID, uid)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_delete:") {
			// 仅超管可以删除管理员 - 显示确认界面
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_delete:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showAdminDeleteConfirm(bot, chatID, msgID, uid)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_unban:") {
			// 仅超管可以封禁/解封 - 显示确认界面
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_unban:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showAdminBanConfirm(bot, chatID, msgID, uid)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_ban_confirm:") {
			// 确认封禁
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_ban_confirm:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleAdminBanToggle(bot, chatID, msgID, uid, false)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_unban_confirm:") {
			// 确认解封
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_unban_confirm:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleAdminBanToggle(bot, chatID, msgID, uid, true)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_ban_cancel:") {
			// 取消封禁/解封，返回管理员详情
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_ban_cancel:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showAdminDetailInline(bot, chatID, msgID, uid)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_delete_confirm:") {
			// 确认删除
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_delete_confirm:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleAdminDelete(bot, chatID, msgID, uid)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_delete_cancel:") {
			// 取消删除，返回管理员详情
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_delete_cancel:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showAdminDetailInline(bot, chatID, msgID, uid)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "adm_remark:") {
			// 仅超管可以设置备注
			if !middleware.CanManageAdmins(userID) {
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⛔ 仅超级管理员可管理管理员列表"))
				return
			}
			uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm_remark:"), 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			beginAdminRemark(userID, uid, bot, chatID, msgID)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if data == "exit" {
			// Delete the message when exit is clicked
			deleteMsg := tgbotapi.NewDeleteMessage(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID)
			_, _ = bot.Request(deleteMsg)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "已退出"))
			return
		}
		if strings.HasPrefix(data, "fwd:") {
			idStr := strings.TrimPrefix(data, "fwd:")
			fid, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			editForwardInfo(bot, chatID, msgID, uint(fid))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "fwd_toggle_ban:") {
			idStr := strings.TrimPrefix(data, "fwd_toggle_ban:")
			fid, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleForwardToggleBan(bot, chatID, uint(fid))
			editForwardInfo(bot, chatID, msgID, uint(fid))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "fwd_check_resolve:") {
			idStr := strings.TrimPrefix(data, "fwd_check_resolve:")
			fid, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleForwardCheckAndResolve(bot, chatID, msgID, uint(fid))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "fwd_get_ip:") {
			idStr := strings.TrimPrefix(data, "fwd_get_ip:")
			fid, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleForwardGetIP(bot, chatID, msgID, uint(fid))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "fwd_delete:") {
			idStr := strings.TrimPrefix(data, "fwd_delete:")
			fid, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showForwardDeleteConfirm(bot, chatID, msgID, uint(fid))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "fwd_delete_confirm:") {
			idStr := strings.TrimPrefix(data, "fwd_delete_confirm:")
			fid, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			handleForwardDelete(bot, chatID, msgID, uint(fid))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "fwd_delete_cancel:") {
			idStr := strings.TrimPrefix(data, "fwd_delete_cancel:")
			fid, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			editForwardInfo(bot, chatID, msgID, uint(fid))
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "add_forward:") {
			idStr := strings.TrimPrefix(data, "add_forward:")
			did, _ := strconv.ParseUint(idStr, 10, 64)
			chatID := update.CallbackQuery.Message.Chat.ID
			msgID := update.CallbackQuery.Message.MessageID
			showAddForwardForm(bot, chatID, msgID, uint(did), userID)
			_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
		if strings.HasPrefix(data, "fwd_edit:") {
			parts := strings.Split(data, ":")
			if len(parts) >= 4 && parts[1] == "show" {
				// fwd_edit:show:<id>:<field> - 显示编辑选项
				fid, _ := strconv.ParseUint(parts[2], 10, 64)
				field := parts[3]
				chatID := update.CallbackQuery.Message.Chat.ID
				msgID := update.CallbackQuery.Message.MessageID

				if field == "type" {
					// 记录类型 - 显示按钮选择
					edit := tgbotapi.NewEditMessageTextAndMarkup(
						chatID,
						msgID,
						"📝 *修改记录类型*\n\n请选择新的记录类型：",
						ForwardEditTypeKeyboard(uint(fid)),
					)
					edit.ParseMode = "Markdown"
					_, _ = bot.Send(edit)
				} else {
					// 其他字段 - 进入文本输入模式
					forwardEditSessions[userID] = ForwardEditSession{
						ForwardID: uint(fid),
						Field:     field,
						ChatID:    chatID,
						MessageID: msgID,
					}
					text := "✒️ 请输入新的值："
					switch field {
					case "domain":
						text = "🌐 *修改转发域名*\n\n请输入新的转发域名："
					case "ip":
						text = "📍 *修改 IP*\n\n请输入新的 IP："
					case "isp":
						text = "🏢 *修改 ISP*\n\n请输入新的 ISP（可为空）："
					case "weight":
						text = "⚖️ *修改权重*\n\n请输入新的权重（数字）："
					case "sort":
						text = "🔢 *修改排序*\n\n请输入新的排序值（数字）："
					}
					edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
					edit.ParseMode = "Markdown"
					_, _ = bot.Send(edit)
				}
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
				return
			} else if len(parts) >= 5 && parts[1] == "value" {
				// fwd_edit:value:<id>:<field>:<value> - 直接设置值
				fid, _ := strconv.ParseUint(parts[2], 10, 64)
				field := parts[3]
				value := parts[4]
				chatID := update.CallbackQuery.Message.Chat.ID
				msgID := update.CallbackQuery.Message.MessageID

				if db.DB == nil {
					if err := db.InitDB(); err != nil {
						edit := tgbotapi.NewEditMessageText(chatID, msgID, "❌ 数据库初始化失败")
						_, _ = bot.Send(edit)
						return
					}
				}

				var f models.ForwardRecord
				if err := db.DB.Where("id = ?", uint(fid)).First(&f).Error; err != nil {
					edit := tgbotapi.NewEditMessageText(chatID, msgID, "❌ 未找到该转发记录")
					_, _ = bot.Send(edit)
					return
				}

				// 设置新值
				if field == "type" {
					f.RecordType = value
				}

				if err := operate.UpdateForwardRecord(db.DB, f); err != nil {
					edit := tgbotapi.NewEditMessageText(chatID, msgID, "❌ 更新失败："+err.Error())
					_, _ = bot.Send(edit)
					return
				}

				// 显示成功并返回详情
				edit := tgbotapi.NewEditMessageText(chatID, msgID, "✅ 修改成功")
				_, _ = bot.Send(edit)
				time.Sleep(1 * time.Second)
				editForwardInfo(bot, chatID, msgID, uint(fid))
				_, _ = bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
				return
			}
			return
		}
		return
	}

	// 消息处理
	if update.Message == nil {
		return
	}

	text := update.Message.Text
	userID = update.Message.From.ID

	utils.Logger.Infof("用户 %d (%s) 接收到的消息: %s", userID, update.Message.From.UserName, text)

	// 检查是否处于管理员备注会话
	if handleAdminRemarkInput(UpdateContext{
		Update:    update,
		Bot:       bot,
		Username:  update.Message.From.UserName,
		UserID:    update.Message.From.ID,
		MessageID: update.Message.MessageID,
	}) {
		return
	}

	// 检查是否处于主域名编辑会话
	if handleDomainEditInput(UpdateContext{
		Update:    update,
		Bot:       bot,
		Username:  update.Message.From.UserName,
		UserID:    update.Message.From.ID,
		MessageID: update.Message.MessageID,
	}) {
		return
	}

	// 检查是否处于转发记录编辑会话
	if handleForwardEditInput(UpdateContext{
		Update:    update,
		Bot:       bot,
		Username:  update.Message.From.UserName,
		UserID:    update.Message.From.ID,
		MessageID: update.Message.MessageID,
	}) {
		return
	}

	// ✅ 再检查命令
	for _, cmd := range Commands {
		if strings.HasPrefix(text, "/"+cmd.Command) {
			ctx := UpdateContext{
				Update:    update,
				Bot:       bot,
				Username:  update.Message.From.UserName,
				LastName:  update.Message.From.LastName,
				FirstName: update.Message.From.FirstName,
				UserID:    userID,
				MessageID: update.Message.MessageID,
			}

			// 权限校验（仅对 RequireAdmin=true 的命令）
			if cmd.RequireAdmin {
				isAdmin, isBanned, err := middleware.IsAdminAndNotBanned(userID)
				if err != nil || !isAdmin {
					SendMessage(ctx, 0, false, "⛔ 权限不足：需要管理员权限")
					return
				}
				if isBanned {
					SendMessage(ctx, 0, false, "⛔ 您的账号已被封禁，无法使用此命令")
					return
				}
			}

			cmd.Handler(ctx)
			return
		}
	}
}
