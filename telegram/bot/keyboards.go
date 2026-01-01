package bot

import (
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/utils"
)

// 管理员列表键盘
func AdminsKeyboard(admins []models.TelegramAdmins) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, a := range admins {
		uidStr := strconv.FormatInt(a.UID, 10)
		// 显示格式：ID + 名字
		name := a.FirstName
		if a.LastName != "" {
			name += " " + a.LastName
		}
		if name == "" {
			name = a.Username
		}
		if name == "" {
			name = "未知"
		}
		text := "👤 " + uidStr + " - " + name
		btn := tgbotapi.NewInlineKeyboardButtonData(text, "adm:"+uidStr)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	// 添加退出按钮
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🚪 退出", "exit"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 管理员详情操作键盘
func AdminActionsKeyboard(a models.TelegramAdmins) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	uid := strconv.FormatInt(a.UID, 10)
	banText := "🚫 封禁"
	banData := "adm_ban:" + uid
	if a.IsBan {
		banText = "✅ 解除封禁"
		banData = "adm_unban:" + uid
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(banText, banData),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📝 设置备注", "adm_remark:"+uid),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🗑️ 删除管理员", "adm_delete:"+uid),
	))
	// 返回管理员列表按钮
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ 返回管理员列表", "back:admins"),
	))
	// 退出按钮
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🚪 退出", "exit"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 管理员封禁确认键盘
func AdminBanConfirmKeyboard(uid int64, isBan bool) tgbotapi.InlineKeyboardMarkup {
	uidStr := strconv.FormatInt(uid, 10)
	rows := [][]tgbotapi.InlineKeyboardButton{}
	if isBan {
		// 当前已封禁，询问是否解封
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ 确认解除封禁", "adm_unban_confirm:"+uidStr),
			tgbotapi.NewInlineKeyboardButtonData("❌ 取消", "adm_ban_cancel:"+uidStr),
		))
	} else {
		// 当前未封禁，询问是否封禁
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ 确认封禁", "adm_ban_confirm:"+uidStr),
			tgbotapi.NewInlineKeyboardButtonData("❌ 取消", "adm_ban_cancel:"+uidStr),
		))
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 管理员删除确认键盘
func AdminDeleteConfirmKeyboard(uid int64) tgbotapi.InlineKeyboardMarkup {
	uidStr := strconv.FormatInt(uid, 10)
	rows := [][]tgbotapi.InlineKeyboardButton{}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✅ 确认删除", "adm_delete_confirm:"+uidStr),
		tgbotapi.NewInlineKeyboardButtonData("❌ 取消", "adm_delete_cancel:"+uidStr),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 使用 ID 的主域名列表键盘
func DomainsKeyboard(domains []models.DomainRecord) tgbotapi.InlineKeyboardMarkup {
	utils.Logger.Infof("[DomainsKeyboard] 开始生成键盘，输入域名数量: %d", len(domains))
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for i, d := range domains {
		// 封禁状态 emoji
		banEmoji := "✅"
		if d.IsDisableCheck {
			banEmoji = "🚫"
		}

		// 查找最后一个成功解析的转发域名
		var lastResolvedForward string
		for _, fwd := range d.Forwards {
			if fwd.ResolveStatus == "success" {
				lastResolvedForward = fwd.ForwardDomain
				// 继续循环找最后一个（最新的）
			}
		}

		// 构建文本
		text := banEmoji + " " + d.Domain + ":" + strconv.Itoa(d.Port)
		if lastResolvedForward != "" {
			text += " >>> " + lastResolvedForward
		}

		data := "dom:" + strconv.FormatUint(uint64(d.ID), 10)
		btn := tgbotapi.NewInlineKeyboardButtonData(text, data)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
		utils.Logger.Infof("[DomainsKeyboard] 添加按钮 %d: %s -> %s", i+1, text, data)
	}
	// 添加退出按钮
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🚪 退出", "exit"),
	))
	utils.Logger.Infof("[DomainsKeyboard] 键盘生成完成，总行数: %d", len(rows))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 主域名详情操作键盘
func DomainActionsKeyboard(d models.DomainRecord) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	idStr := strconv.FormatUint(uint64(d.ID), 10)

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✏️ 修改域名", "dom_edit:"+idStr+":name"),
		tgbotapi.NewInlineKeyboardButtonData("🔌 修改端口", "dom_edit:"+idStr+":port"),
	))

	checkText := "✅ 检测:开启"
	if d.IsDisableCheck {
		checkText = "🚫 检测:关闭"
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔢 修改排序", "dom_edit:"+idStr+":sort"),
		tgbotapi.NewInlineKeyboardButtonData(checkText, "dom_toggle_check:"+idStr),
	))

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 查看转发域名", "dom_forwards:"+idStr),
		tgbotapi.NewInlineKeyboardButtonData("🗑️ 删除主域名", "dom_delete:"+idStr),
	))

	// 返回到主域名列表
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ 返回主域名列表", "back:domains"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 转发列表键盘（使用转发记录 ID）
func ForwardListKeyboard(forwards []models.ForwardRecord, domainID uint) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, f := range forwards {
		idStr := strconv.FormatUint(uint64(f.ID), 10)
		data := "fwd:" + idStr
		// 封禁状态 emoji
		banEmoji := "✅"
		if f.IsBan {
			banEmoji = "🚫"
		}
		text := banEmoji + " " + f.ForwardDomain
		btn := tgbotapi.NewInlineKeyboardButtonData(text, data)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	// 添加转发域名按钮
	addForwardStr := strconv.FormatUint(uint64(domainID), 10)
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ 添加转发域名", "add_forward:"+addForwardStr),
	))
	// 返回主域名详情
	domainIDStr := strconv.FormatUint(uint64(domainID), 10)
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ 返回主域名", "back:domain:"+domainIDStr),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 转发详情操作键盘
func ForwardActionsKeyboard(f models.ForwardRecord) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	idStr := strconv.FormatUint(uint64(f.ID), 10)

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🌐 修改转发域名", "fwd_edit:show:"+idStr+":domain"),
		tgbotapi.NewInlineKeyboardButtonData("🔍 获取 IP", "fwd_get_ip:"+idStr),
	))

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🏢 修改 ISP", "fwd_edit:show:"+idStr+":isp"),
		tgbotapi.NewInlineKeyboardButtonData("⚖️ 修改权重", "fwd_edit:show:"+idStr+":weight"),
	))

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔢 修改排序", "fwd_edit:show:"+idStr+":sort"),
		tgbotapi.NewInlineKeyboardButtonData("📝 修改记录类型", "fwd_edit:show:"+idStr+":type"),
	))

	// 新增：检测并解析按钮
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔍 检测并解析", "fwd_check_resolve:"+idStr),
	))

	banText := "🚫 封禁:关闭"
	if f.IsBan {
		banText = "✅ 封禁:开启"
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(banText, "fwd_toggle_ban:"+idStr),
		tgbotapi.NewInlineKeyboardButtonData("🗑️ 删除转发", "fwd_delete:"+idStr),
	))

	// 返回该主域名的转发列表
	domainIDStr := strconv.FormatUint(uint64(f.DomainRecordID), 10)
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ 返回转发列表", "back:forwards:"+domainIDStr),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// 转发编辑选项键盘（记录类型）
func ForwardEditTypeKeyboard(forwardID uint) tgbotapi.InlineKeyboardMarkup {
	idStr := strconv.FormatUint(uint64(forwardID), 10)
	rows := [][]tgbotapi.InlineKeyboardButton{}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📝 A 记录", "fwd_edit:value:"+idStr+":type:A"),
		tgbotapi.NewInlineKeyboardButtonData("📝 CNAME 记录", "fwd_edit:value:"+idStr+":type:CNAME"),
	))

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ 返回", "fwd:"+idStr),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
