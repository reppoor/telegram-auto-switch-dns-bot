package bot

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"strconv"
	"strings"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/utils"
)

// 消息解析模式枚举
const (
	ParseModeNone       = 0
	ParseModeMarkdown   = 1
	ParseModeMarkdownV2 = 2
	ParseModeHTML       = 3
)

// 检查markdown语法
func escapeMarkdownV2(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(s)
}

// Markdown (v1) 转义，用于 admin 详情显示
func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"`", "\\`",
	)
	return replacer.Replace(s)
}

// SendMessage 封装发送消息
// parseModeFlag: 0=普通文本, 1=Markdown, 2=MarkdownV2, 3=HTML
// disableNotification: 是否静默发送
// format + args: 支持 fmt.Sprintf 风格
func SendMessage(ctx UpdateContext, parseModeFlag int, disableNotification bool, format string, args ...interface{}) {
	text := fmt.Sprintf(format, args...)

	msg := tgbotapi.NewMessage(ctx.Update.Message.Chat.ID, text)
	msg.DisableNotification = disableNotification

	// 根据数字选择 ParseMode
	switch parseModeFlag {
	case ParseModeMarkdown:
		msg.ParseMode = "Markdown"
	case ParseModeMarkdownV2:
		msg.ParseMode = "MarkdownV2"
	case ParseModeHTML:
		msg.ParseMode = "HTML"
	default:
		msg.ParseMode = ""
	}

	if _, err := ctx.Bot.Send(msg); err != nil {
		utils.Logger.Warnf("发送消息失败: %v", err)
	}
	// ✅ 发送成功也记录
	utils.Logger.Infof("发送消息成功: 用户 %d (%s) | 消息: %s | MessageID: %d",
		ctx.UserID, ctx.Username, text, ctx.MessageID)
}

// ParseBatchUploadContent 解析批量上传的文件内容
func parseBatchUploadContent(content string) ([]models.DomainRecord, error) {
	lines := strings.Split(content, "\n")
	domainMap := make(map[string]*models.DomainRecord) // 合并相同主域名+端口

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // 跳过空行和注释
		}

		parts := strings.Split(line, "|")
		if len(parts) < 11 {
			return nil, fmt.Errorf("第 %d 行格式错误：字段数不足（需要 11 个字段）", lineNum+1)
		}

		// 解析字段
		domain := strings.TrimSpace(parts[0])
		portStr := strings.TrimSpace(parts[1])
		isDisableStr := strings.TrimSpace(parts[2])
		sortOrderStr := strings.TrimSpace(parts[3])
		forwardDomain := strings.TrimSpace(parts[4])
		ip := strings.TrimSpace(parts[5])
		isp := strings.TrimSpace(parts[6])
		isBanStr := strings.TrimSpace(parts[7])
		weightStr := strings.TrimSpace(parts[8])
		forwardSortStr := strings.TrimSpace(parts[9])
		recordType := strings.TrimSpace(parts[10])

		// 类型转换
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行端口号错误: %v", lineNum+1, err)
		}

		isDisable := strings.EqualFold(isDisableStr, "true")

		sortOrder, err := strconv.Atoi(sortOrderStr)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行排序号错误: %v", lineNum+1, err)
		}

		isBan := strings.EqualFold(isBanStr, "true")

		weight, err := strconv.Atoi(weightStr)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行权重错误: %v", lineNum+1, err)
		}

		forwardSort, err := strconv.Atoi(forwardSortStr)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行转发排序号错误: %v", lineNum+1, err)
		}

		if recordType == "" {
			recordType = "A" // 默认值
		}

		// 创建转发记录
		forward := models.ForwardRecord{
			ForwardDomain: forwardDomain,
			IP:            ip,
			ISP:           isp,
			IsBan:         isBan,
			Weight:        weight,
			SortOrder:     forwardSort,
			RecordType:    recordType,
		}

		// 合并键：domain+port
		key := fmt.Sprintf("%s:%d", domain, port)
		// 检查主域名+端口是否已存在
		if domainRecord, exists := domainMap[key]; exists {
			// 合并到现有主域名+端口
			domainRecord.Forwards = append(domainRecord.Forwards, forward)
		} else {
			// 创建新主域名+端口
			domainMap[key] = &models.DomainRecord{
				Domain:         domain,
				Port:           port,
				IsDisableCheck: isDisable,
				SortOrder:      sortOrder,
				Forwards:       []models.ForwardRecord{forward},
			}
		}
	}

	// 转换为切片
	result := make([]models.DomainRecord, 0, len(domainMap))
	for _, domain := range domainMap {
		result = append(result, *domain)
	}

	return result, nil
}
