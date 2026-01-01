package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"telegram-auto-switch-dns-bot/db"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/db/operate"
	"telegram-auto-switch-dns-bot/utils"
)

func startHandler(ctx UpdateContext) {
	welcomeMsg := "🤖 *Telegram Auto Switch DNS Bot*\n\n" +
		"🚀 _Version 1\\.0\\.0_\n\n" +
		"💡 使用 */help* 查看所有可用命令\n" +
		"🔧 技术支持: @YourSupport"

	SendMessage(ctx, 2, false, welcomeMsg)
}
func idHandler(ctx UpdateContext) {
	user := ctx.Update.Message.From

	// 1️⃣ 获取基础信息
	admin := models.TelegramAdmins{
		UID:       user.ID,
		Username:  user.UserName,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Role:      "admin", // 默认角色，可以按需求修改
		AddedBy:   0,       // 自己添加自己，可以为 0 或者 ctx.UserID
		IsBan:     true,    // 默认封禁
	}

	// 2️⃣ 先检查数据库，决定是否写入（已弃用缓存）
	if db.DB == nil {
		_ = db.InitDB()
	}
	if db.DB != nil {
		// 直接从数据库获取
		existingAdmin, err := operate.GetAdministrator(db.DB, user.ID)
		if err != nil {
			// 数据库中不存在，写入数据库
			if errors.Is(err, operate.ErrAdminNotFound) {
				if err := operate.AddAdministrator(db.DB, admin); err != nil {
					SendMessage(ctx, 0, false, fmt.Sprintf("❌ 添加管理员失败: %v", err))
					return
				}
				utils.Logger.Infof("✅ 用户 %d 首次使用 /id，已写入 TelegramAdmins 表（默认封禁）", user.ID)
			} else {
				// 其他错误
				utils.Logger.Warnf("⚠️ 查询用户 %d 时出错: %v", user.ID, err)
			}
		} else {
			// 已存在（从数据库获取），跳过写入
			utils.Logger.Infof("✅ 用户 %d 已存在（UID: %d），跳过写入", user.ID, existingAdmin.UID)
		}
	}

	// 3️⃣ 准备消息文本
	msgText := fmt.Sprintf(
		"👤 用户信息:\n\n"+
			"Telegram ID: `%d`\n"+
			"用户名: @%s\n"+
			"名字: `%s %s`\n"+
			"语言: `%s`\n",
		user.ID,
		escapeMarkdownV2(user.UserName),
		escapeMarkdownV2(user.FirstName),
		escapeMarkdownV2(user.LastName),
		escapeMarkdownV2(user.LanguageCode),
	)

	// 4️⃣ 发送给用户（MarkdownV2 格式）
	SendMessage(ctx, 2, true, msgText)
}
func helpHandler(ctx UpdateContext) {
	helpText := "🤖 可用命令列表:\n"
	for _, cmd := range Commands {
		name := strings.TrimPrefix(cmd.Command, "/") // 保留原字符，不转义
		helpText += fmt.Sprintf("/%s - %s\n", name, escapeMarkdownV2(cmd.Description))
	}
	SendMessage(ctx, 0, true, helpText)
}
func getAminHandler(ctx UpdateContext) {
	userID := ctx.UserID // 假设 UpdateContext 有 UserID 字段

	// 1️⃣ 调用 GetAdministrator 获取管理员信息
	admin, err := operate.GetAdministrator(db.DB, userID)
	if err != nil {
		if errors.Is(err, operate.ErrAdminNotFound) {
			SendMessage(ctx, 0, false, "❌ 您不是管理员或管理员信息不存在")
		} else {
			SendMessage(ctx, 0, false, fmt.Sprintf("❌ 获取管理员信息失败: %v", err))
		}
		return
	}

	// 2️⃣ 构建输出文本
	msgText := fmt.Sprintf(
		"👤 管理员信息:\n\n"+
			"UID: `%d`\n"+
			"用户名: @%s\n"+
			"名字: `%s %s`\n"+
			"角色: `%s`\n"+
			"备注: `%s`\n",
		admin.UID,
		escapeMarkdownV2(admin.Username),
		escapeMarkdownV2(admin.FirstName),
		escapeMarkdownV2(admin.LastName),
		admin.Role,
		escapeMarkdownV2(admin.Remark),
	)

	// 3️⃣ 发送消息
	SendMessage(ctx, 2, true, msgText)
}

// UploadDomainsHandler 批量导入入口（命令行形式）
func UploadDomainsHandler(ctx UpdateContext) {
	text := strings.TrimSpace(ctx.Update.Message.Text)
	parts := strings.SplitN(text, " ", 2)

	// 如果只是命令本身，显示帮助信息
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		SendMessage(ctx, 2, false,
			"📄 批量导入域名信息\n\n"+
				"使用方法：\n"+
				"%s\n\n"+
				"数据格式（每行一条记录）：\n"+
				"%s\n\n"+
				"示例：\n"+
				"%s\n\n"+
				"📌 说明：\n"+
				"%s",
			escapeMarkdownV2("/upload_domains <数据>"),
			"`domain\\|port\\|is\\_disable\\|sort\\_order\\|forward\\_domain\\|ip\\|isp\\|is\\_ban\\|weight\\|forward\\_sort\\|record\\_type`",
			"`/upload\\_domains main\\.example\\.com\\|80\\|false\\|1\\|forward\\.example\\.com\\|0\\.0\\.0\\.0\\|电信\\|false\\|10\\|1\\|A\nmain\\.example\\.com\\|80\\|false\\|1\\|forward\\.example\\.com\\|0\\.0\\.0\\.0\\|联通\\|false\\|20\\|2\\|A`",
			escapeMarkdownV2("- DNS ID 会自动从 Cloudflare 获取，请确保域名在 Cloudflare 中已存在\n- 相同的 domain 会自动合并为一个主域名\n- is_disable 和 is_ban 使用 true/false\n- isp 可留空\n- record_type 默认为 A，也可以是 CNAME"))
		return
	}

	// 获取数据部分
	data := strings.TrimSpace(parts[1])
	utils.Logger.Infof("用户 %d 批量导入数据，长度: %d", ctx.UserID, len(data))

	// 解析数据
	domains, err := parseBatchUploadContent(data)
	if err != nil {
		utils.Logger.Errorf("解析数据失败: %v", err)
		SendMessage(ctx, 0, false, fmt.Sprintf("❌ 解析数据失败：\n%v", err))
		return
	}

	if len(domains) == 0 {
		SendMessage(ctx, 0, false, "⚠️ 数据中没有有效记录。")
		return
	}

	utils.Logger.Infof("解析成功，共 %d 条主域名", len(domains))

	// 保存到数据库（已弃用缓存）
	jsonBytes, _ := json.Marshal(domains)
	if err := operate.SaveToDBOnly(db.DB, string(jsonBytes)); err != nil {
		utils.Logger.Errorf("保存失败: %v", err)
		SendMessage(ctx, 0, false, fmt.Sprintf("❌ 保存失败：\n%v", err))
	} else {
		utils.Logger.Infof("批量导入保存成功")
		SendMessage(ctx, 0, false,
			fmt.Sprintf("🎉 批量导入成功！\n\n"+
				"✅ 已成功导入 %d 条主域名记录\n"+
				"💾 数据已保存到数据库", len(domains)))
	}
}

// ExportDomainData 导出域名数据
func ExportDomainData() (string, error) {
	// 从数据库获取所有域名记录
	var domains []models.DomainRecord
	if err := db.DB.Preload("Forwards").Order("sort_order asc, id asc").Find(&domains).Error; err != nil {
		utils.Logger.Errorf("获取域名记录失败: %v", err)
		return "", fmt.Errorf("获取域名记录失败: %v", err)
	}

	if len(domains) == 0 {
		return "", fmt.Errorf("没有可导出的域名记录")
	}

	var result strings.Builder

	for _, domain := range domains {
		for _, forward := range domain.Forwards {
			// 格式: domain|port|is_disable|sort_order|forward_domain|ip|isp|is_ban|weight|forward_sort|record_type
			line := fmt.Sprintf("%s|%d|%t|%d|%s|%s|%s|%t|%d|%d|%s\n",
				domain.Domain,
				domain.Port,
				domain.IsDisableCheck,
				domain.SortOrder,
				forward.ForwardDomain,
				forward.IP,
				forward.ISP,
				forward.IsBan,
				forward.Weight,
				forward.SortOrder,
				forward.RecordType,
			)
			result.WriteString(line)
		}
	}

	return result.String(), nil
}

// ExportDomainsHandler 导出域名数据处理器
func ExportDomainsHandler(ctx UpdateContext) {
	utils.Logger.Infof("用户 %d 请求导出域名数据", ctx.UserID)

	// 导出数据
	exportData, err := ExportDomainData()
	if err != nil {
		utils.Logger.Errorf("导出数据失败: %v", err)
		SendMessage(ctx, 0, false, fmt.Sprintf("❌ 导出失败：\n%v", err))
		return
	}

	// 如果数据为空
	if exportData == "" {
		SendMessage(ctx, 0, false, "⚠️ 没有可导出的域名记录。")
		return
	}

	// 发送导出的数据（使用等宽字体）
	SendMessage(ctx, 2, false, fmt.Sprintf("📤 *域名数据导出结果*：\n\n`%s`", strings.ReplaceAll(exportData, "\n", "\n`\n`")))
}
