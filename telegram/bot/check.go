package bot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/db"

	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/db/operate"
	"telegram-auto-switch-dns-bot/utils"
)

// 统一后端检测请求/响应结构
type tcpCheckRequest struct {
	Target string `json:"target"`
	Port   int    `json:"port"`
	Key    string `json:"key"`
}

type tcpCheckResponseData struct {
	Result          bool   `json:"result"`
	Target          string `json:"target"`
	TargetIp        string `json:"target_ip"`
	Message         string `json:"message"`
	BackendPublicIP string `json:"backend_public_ip"`
}

type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// checkConnectivityWithProgress 调用后端 /api/v1/tcp_checks（带进度回调）
func checkConnectivityWithProgress(target string, port int, progressCallback func(current int, total int)) (tcpCheckResponseData, error) {
	backend := strings.TrimRight(config.Global.BackendURL.Api, "/")
	url := backend + "/api/v1/tcp_checks"

	// 构建请求体
	payload := tcpCheckRequest{
		Target: target,
		Port:   port,
		Key:    config.Global.BackendListen.Key,
	}
	buf, _ := json.Marshal(payload)

	// 发送 POST 请求（流式）
	client := &http.Client{Timeout: config.Global.BackendURL.Timeout * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return tcpCheckResponseData{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return tcpCheckResponseData{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 流式读取响应
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// 解析每行 JSON 响应
		var apiResp apiResponse
		if err := json.Unmarshal([]byte(line), &apiResp); err != nil {
			utils.Logger.Warnf("Failed to parse response line: %s, error: %v", line, err)
			continue
		}

		// 如果是进度消息 (Code=1)，调用进度回调
		if apiResp.Code == 1 && apiResp.Message == "progress" && progressCallback != nil {
			// 解析进度数据
			if data, ok := apiResp.Data.(map[string]interface{}); ok {
				if current, ok1 := data["current"].(float64); ok1 {
					if total, ok2 := data["total"].(float64); ok2 {
						progressCallback(int(current), int(total))
					}
				}
			}
			continue
		}

		// 如果是最终结果 (Code=0)
		if apiResp.Code == 0 {
			// 将 Data 转换为 tcpCheckResponseData
			dataBytes, err := json.Marshal(apiResp.Data)
			if err != nil {
				return tcpCheckResponseData{}, fmt.Errorf("failed to marshal data: %v", err)
			}

			var result tcpCheckResponseData
			if err := json.Unmarshal(dataBytes, &result); err != nil {
				return tcpCheckResponseData{}, fmt.Errorf("failed to unmarshal data: %v", err)
			}

			return result, nil
		}

		// 其他错误情况 (非进度消息且 Code != 0)
		if apiResp.Code != 0 {
			// 特别处理进度消息被错误识别为错误的情况
			if apiResp.Message == "progress" {
				// 这应该是进度消息而不是错误，继续处理
				continue
			}

			// 真正的错误情况
			// 尝试将 Data 转换为 tcpCheckResponseData
			dataBytes, err := json.Marshal(apiResp.Data)
			if err != nil {
				return tcpCheckResponseData{}, fmt.Errorf("backend error: %s", apiResp.Message)
			}

			var result tcpCheckResponseData
			if err := json.Unmarshal(dataBytes, &result); err != nil {
				return tcpCheckResponseData{}, fmt.Errorf("backend error: %s", apiResp.Message)
			}

			return result, fmt.Errorf("backend error: %s", apiResp.Message)
		}
	}

	return tcpCheckResponseData{}, fmt.Errorf("unexpected response format")
}

// listDomainsHandler 列出所有主域名（命令入口）
func listDomainsHandler(ctx UpdateContext) {
	// 确保 DB 初始化
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			SendMessage(ctx, 0, false, "数据库未初始化: %v", err)
			return
		}
	}

	chatID := ctx.Update.Message.Chat.ID
	sendDomainList(ctx.Bot, chatID)
}

// sendDomainList 实际查询并发送主域名列表
func sendDomainList(bot *tgbotapi.BotAPI, chatID int64) {
	// 直接从数据库读取所有主域名
	var domains []models.DomainRecord
	if err := db.DB.Preload("Forwards", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("sort_order asc, id asc")
	}).Order("sort_order asc, id asc").Find(&domains).Error; err != nil {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("获取域名列表失败: %v", err))
		_, _ = bot.Send(msg)
		return
	}
	utils.Logger.Infof("[ListDomains] ✅ 从数据库读取到 %d 条主域名记录", len(domains))

	if len(domains) == 0 {
		msg := tgbotapi.NewMessage(chatID, "当前没有配置任何主域名。")
		_, _ = bot.Send(msg)
		return
	}

	utils.Logger.Infof("[ListDomains] 准备生成键盘，域名数量: %d", len(domains))
	for i, d := range domains {
		utils.Logger.Infof("[ListDomains] 域名 %d: ID=%d, Domain=%s:%d", i+1, d.ID, d.Domain, d.Port)
	}

	kb := DomainsKeyboard(domains)
	utils.Logger.Infof("[ListDomains] 键盘生成完成，按钮行数: %d", len(kb.InlineKeyboard))

	msg := tgbotapi.NewMessage(chatID, "🏛 *主域名列表*\n\n请选择一个主域名进行管理：")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	if _, err := bot.Send(msg); err != nil {
		utils.Logger.Warnf("发送域名列表失败: %v", err)
	}
}

// editDomainList 编辑当前消息的主域名列表
func editDomainList(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	// 直接从数据库读取所有主域名
	var domains []models.DomainRecord
	if err := db.DB.Preload("Forwards", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("sort_order asc, id asc")
	}).Order("sort_order asc, id asc").Find(&domains).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("获取域名列表失败: %v", err))
		_, _ = bot.Send(edit)
		return
	}
	utils.Logger.Infof("[EditDomainList] ✅ 从数据库读取到 %d 条主域名记录", len(domains))

	if len(domains) == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "当前没有配置任何主域名。")
		_, _ = bot.Send(edit)
		return
	}

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, "🏛 *主域名列表*\n\n请选择一个主域名进行管理：", DomainsKeyboard(domains))
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}

// 编辑当前消息的某主域名转发列表
func editForwards(bot *tgbotapi.BotAPI, chatID int64, messageID int, domainID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, "数据库初始化失败："+err.Error())
			_, _ = bot.Send(edit)
			return
		}
	}

	var d models.DomainRecord
	if err := db.DB.Preload("Forwards", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("sort_order asc, id asc")
	}).Where("id = ?", domainID).First(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "获取转发列表失败："+err.Error())
		_, _ = bot.Send(edit)
		return
	}

	if len(d.Forwards) == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "ℹ️ 该主域名暂无转发域名记录")
		_, _ = bot.Send(edit)
		return
	}

	kb := ForwardListKeyboard(d.Forwards, domainID)
	text := fmt.Sprintf("📋 *转发域名列表*\n\n主域名: `%s:%d`\n\n请选择一个转发域名:", d.Domain, d.Port)
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, kb)
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}

// 编辑当前消息的转发详情
func editForwardInfo(bot *tgbotapi.BotAPI, chatID int64, messageID int, forwardID uint) {
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, "数据库初始化失败："+err.Error())
			_, _ = bot.Send(edit)
			return
		}
	}

	var f models.ForwardRecord
	var d models.DomainRecord
	// 直接从数据库查询
	if err := db.DB.Where("id = ?", forwardID).First(&f).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到转发记录")
		_, _ = bot.Send(edit)
		return
	}

	// 获取对应的主域名
	if err := db.DB.Preload("Forwards").Where("id = ?", f.DomainRecordID).First(&d).Error; err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到对应的主域名")
		_, _ = bot.Send(edit)
		return
	}

	if f.ID == 0 {
		if err := db.DB.Where("id = ?", forwardID).First(&f).Error; err != nil {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, "未找到该转发记录")
			_, _ = bot.Send(edit)
			return
		}

	}
	if d.ID == 0 {

		if d.ID == 0 {
			if err := db.DB.Where("id = ?", f.DomainRecordID).First(&d).Error; err != nil {
				edit := tgbotapi.NewEditMessageText(chatID, messageID, "获取主域名信息失败："+err.Error())
				_, _ = bot.Send(edit)
				return
			}

		}
	}

	status := "✅ 未封禁"
	banTimeText := "-"
	if f.IsBan {
		if f.BanTime > 0 {
			// 检查是否已过期
			if time.Now().Unix() > f.BanTime {
				// 自动解除封禁
				if err := operate.AutoUnbanForward(db.DB, &f); err != nil {
					utils.Logger.Errorf("自动解除封禁失败: %v", err)
				}
				status = "✅ 未封禁"
				banTimeText = "已自动解除"
			} else {
				status = "🚫 已封禁"
				banTimeText = time.Unix(f.BanTime, 0).Format("2006-01-02 15:04:05")
			}
		} else {
			status = "🚫 已封禁"
			banTimeText = "永久"
		}
	}

	text := fmt.Sprintf(
		"🔎 *转发详情*\n\n"+
			"*ID*: `%d`\n"+
			"*主域名*: `%s:%d`\n"+
			"*转发域名*: `%s`\n"+
			"*IP*: `%s`\n"+
			"*ISP*: `%s`\n"+
			"*封禁状态*: `%s`\n"+
			"*封禁时间*: `%s`\n"+
			"*权重*: `%d`\n"+
			"*排序*: `%d`\n"+
			"*记录类型*: `%s`",
		f.ID, d.Domain, d.Port, f.ForwardDomain, f.IP, f.ISP, status, banTimeText, f.Weight, f.SortOrder, f.RecordType,
	)
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, ForwardActionsKeyboard(f))
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}
