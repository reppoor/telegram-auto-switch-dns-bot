package bot

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"
	"telegram-auto-switch-dns-bot/cloudflare"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/db"

	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/db/operate"
	"telegram-auto-switch-dns-bot/utils"
)

// 接口调用失败次数计数器
var (
	apiFailureCount int
	apiFailureMutex sync.Mutex
)

// getApiFailureThreshold 获取 API 失败阈值
func getApiFailureThreshold() int {
	return config.Global.AutoCheck.ApiFail
}

// 自动检测任务
var autoCheckRunning = false

// CheckReport 检测报告结构
type CheckReport struct {
	FailedDomains       []string        // 检测失败的主域名
	DisconnectedDomains []DomainFailure // 无法连通的主域名
	BannedForwards      []string        // 被封禁的转发域名
	SwitchedDomains     []DomainSwitch  // DNS 切换成功的主域名
	NoForwardDomains    []string        // 无可用转发的主域名
}

type DomainFailure struct {
	Domain string
	Port   int
	Reason string
}

type DomainSwitch struct {
	Domain        string
	Port          int
	RecordType    string
	NewRecord     string
	ForwardDomain string
	ISP           string
	Weight        int
}

// StartAutoCheck 启动自动检测定时任务
func StartAutoCheck(bot *tgbotapi.BotAPI, interval time.Duration) {
	if autoCheckRunning {
		utils.Logger.Info("⚠️ 自动检测任务已在运行中")
		return
	}
	autoCheckRunning = true
	utils.Logger.Info("🚀 自动检测任务已启动")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	go performAutoCheck(bot)

	for range ticker.C {
		go performAutoCheck(bot)
	}
}

// performAutoCheck 执行一次完整的自动检测
func performAutoCheck(bot *tgbotapi.BotAPI) {
	utils.Logger.Info("📊 开始执行自动检测任务...")

	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			utils.Logger.Errorf("❌ 数据库初始化失败: %v", err)
			return
		}
	}

	// 创建检测报告
	report := &CheckReport{
		FailedDomains:       []string{},
		DisconnectedDomains: []DomainFailure{},
		BannedForwards:      []string{},
		SwitchedDomains:     []DomainSwitch{},
		NoForwardDomains:    []string{},
	}

	// 直接从数据库获取所有主域名（已弃用缓存）
	var domains []models.DomainRecord
	if err := db.DB.Preload("Forwards", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("weight desc, sort_order asc, id asc")
	}).Order("sort_order asc, id asc").Find(&domains).Error; err != nil {
		utils.Logger.Errorf("❌ 获取主域名列表失败: %v", err)
		return
	}
	utils.Logger.Infof("✅ 从数据库读取到 %d 条主域名记录", len(domains))

	utils.Logger.Infof("📋 共获取到 %d 个主域名", len(domains))

	for _, d := range domains {
		// 跳过被禁用检测的主域名
		if d.IsDisableCheck {
			utils.Logger.Infof("⏭️ 跳过主域名 %s:%d (检测已禁用)", d.Domain, d.Port)
			continue
		}

		utils.Logger.Infof("🔍 检测主域名: %s:%d", d.Domain, d.Port)
		checkDomain(d, report)
	}

	// 发送汇总报告
	sendReport(bot, report)

	utils.Logger.Info("✅ 自动检测任务执行完毕")
}

// checkDomain 检测单个主域名及其转发池
func checkDomain(d models.DomainRecord, report *CheckReport) {
	// 1. 检测主域名连通性（带连接进度）
	utils.Logger.Infof("🔍 检测主域名: %s:%d", d.Domain, d.Port)
	result, err := checkConnectivityWithProgress(d.Domain, d.Port, nil)
	if err != nil {
		utils.Logger.Warnf("⚠️ 主域名 %s:%d 检测失败: %v", d.Domain, d.Port, err)
		// 更新 API 失败计数
		incrementApiFailureCount()
		// 记录到报告
		report.FailedDomains = append(report.FailedDomains, fmt.Sprintf("%s:%d", d.Domain, d.Port))
		// 接口调用失败，不继续检测，直接返回
		return
	}

	// 检测成功，重置 API 失败计数
	resetApiFailureCount()

	// 2. 主域名连通正常
	if result.Result {
		utils.Logger.Infof("✅ 主域名 %s:%d 连通正常", d.Domain, d.Port)
		return
	}

	// 3. 主域名不通，记录到报告
	utils.Logger.Warnf("❌ 主域名 %s:%d 无法连通", d.Domain, d.Port)
	// 简化消息内容
	failReason := "无法连接"
	if result.Message != "" {
		if strings.Contains(result.Message, "timeout") || strings.Contains(result.Message, "i/o timeout") {
			failReason = "连接超时"
		} else if strings.Contains(result.Message, "refused") {
			failReason = "连接被拒绝"
		} else if strings.Contains(result.Message, "no route") {
			failReason = "网络不可达"
		}
	}
	report.DisconnectedDomains = append(report.DisconnectedDomains, DomainFailure{
		Domain: d.Domain,
		Port:   d.Port,
		Reason: failReason,
	})

	// 4. 检测转发池
	checkForwardPool(d, report)
}

// checkDomainWithProgress 检测单个主域名及其转发池（带进度回调）
func checkDomainWithProgress(d models.DomainRecord, report *CheckReport, progressCallback func(current int, total int, forwardDomain string)) {
	// 1. 检测主域名连通性（带连接进度）
	utils.Logger.Infof("🔍 检测主域名: %s:%d", d.Domain, d.Port)
	result, err := checkConnectivityWithProgress(d.Domain, d.Port, func(current int, total int) {
		// 调用进度回调显示连接进度
		progressCallback(current, total, fmt.Sprintf("正在检测第 %d/%d 次连接：%s:%d", current, total, d.Domain, d.Port))
	})
	if err != nil {
		utils.Logger.Warnf("⚠️ 主域名 %s:%d 检测失败: %v", d.Domain, d.Port, err)
		// 记录到报告
		report.FailedDomains = append(report.FailedDomains, fmt.Sprintf("%s:%d", d.Domain, d.Port))
		// 接口调用失败，不继续检测，直接返回
		return
	}

	// 2. 主域名连通正常
	if result.Result {
		utils.Logger.Infof("✅ 主域名 %s:%d 连通正常", d.Domain, d.Port)
		return
	}

	// 3. 主域名不通，记录到报告
	utils.Logger.Warnf("❌ 主域名 %s:%d 无法连通", d.Domain, d.Port)
	// 简化消息内容
	failReason := "无法连接"
	if result.Message != "" {
		if strings.Contains(result.Message, "timeout") || strings.Contains(result.Message, "i/o timeout") {
			failReason = "连接超时"
		} else if strings.Contains(result.Message, "refused") {
			failReason = "连接被拒绝"
		} else if strings.Contains(result.Message, "no route") {
			failReason = "网络不可达"
		}
	}
	report.DisconnectedDomains = append(report.DisconnectedDomains, DomainFailure{
		Domain: d.Domain,
		Port:   d.Port,
		Reason: failReason,
	})

	// 4. 检测转发池
	checkForwardPoolWithProgress(d, report, progressCallback)
}

// checkForwardPool 检测转发池并更新到 Cloudflare
func checkForwardPool(d models.DomainRecord, report *CheckReport) {
	if len(d.Forwards) == 0 {
		utils.Logger.Warnf("⚠️ 主域名 %s:%d 无转发记录", d.Domain, d.Port)
		// 记录到报告
		report.NoForwardDomains = append(report.NoForwardDomains, fmt.Sprintf("%s:%d", d.Domain, d.Port))
		return
	}

	// 按权重从大到小排序
	forwards := make([]models.ForwardRecord, len(d.Forwards))
	copy(forwards, d.Forwards)
	sort.Slice(forwards, func(i, j int) bool {
		if forwards[i].Weight != forwards[j].Weight {
			return forwards[i].Weight > forwards[j].Weight
		}
		return forwards[i].SortOrder < forwards[j].SortOrder
	})

	utils.Logger.Infof("🔄 转发池共 %d 个域名，开始按权重检测", len(forwards))

	var availableForward *models.ForwardRecord
	var resolvedIP string // 保存后端接口返回的实际 IP
	var bannedForwards []string

	// 检测每个转发域名
	for i, f := range forwards {
		// 跳过已封禁的转发域名
		if f.IsBan {
			utils.Logger.Infof("⏭️ 跳过已封禁的转发域名: %s (权重: %d)", f.ForwardDomain, f.Weight)
			continue
		}

		utils.Logger.Infof("🔍 [%d/%d] 检测转发域名: %s (权重: %d)", i+1, len(forwards), f.ForwardDomain, f.Weight)

		// 检测连通性（带连接进度）
		result, err := checkConnectivityWithProgress(f.ForwardDomain, d.Port, nil)
		if err != nil {
			utils.Logger.Warnf("⚠️ 转发域名 %s 检测失败: %v", f.ForwardDomain, err)
			// 更新 API 失败计数
			incrementApiFailureCount()
			banForward24Hours(&f)
			bannedForwards = append(bannedForwards, f.ForwardDomain)
			continue
		}

		// 检测成功，重置 API 失败计数
		resetApiFailureCount()

		// 检查检测结果
		if !result.Result {
			// 检查是否是因为连接超时导致的失败
			if strings.Contains(result.Message, "检测结束") && strings.Contains(result.Message, "无法连接") {
				utils.Logger.Warnf("❌ 转发域名 %s 5次连接测试全部失败，进行24小时封禁", f.ForwardDomain)
				banForward24Hours(&f)
				bannedForwards = append(bannedForwards, f.ForwardDomain)
				continue
			} else {
				// 其他原因导致的失败，不封禁
				utils.Logger.Warnf("⚠️ 转发域名 %s 检测失败，但不是因为5次连接全部失败: %s", f.ForwardDomain, result.Message)
			}
		} else {
			// 找到可用的转发域名
			utils.Logger.Infof("✅ 转发域名 %s 连通正常 (IP: %s)", f.ForwardDomain, result.TargetIp)
			availableForward = &f
			resolvedIP = result.TargetIp // 保存后端解析的实际 IP
			break
		}
	}

	// 通知封禁情况
	if len(bannedForwards) > 0 {
		report.BannedForwards = append(report.BannedForwards, bannedForwards...)
	}

	// 如果找到可用的转发域名，更新到 Cloudflare
	if availableForward != nil {
		updateToCloudflare(d, *availableForward, resolvedIP, report)
	} else {
		utils.Logger.Errorf("❌ 主域名 %s:%d 无可用转发域名", d.Domain, d.Port)
		// 记录到报告
		report.NoForwardDomains = append(report.NoForwardDomains, fmt.Sprintf("%s:%d", d.Domain, d.Port))
	}
}

// checkForwardPoolWithProgress 检测转发池并更新到 Cloudflare（带进度回调）
func checkForwardPoolWithProgress(d models.DomainRecord, report *CheckReport, progressCallback func(current int, total int, forwardDomain string)) {
	if len(d.Forwards) == 0 {
		utils.Logger.Warnf("⚠️ 主域名 %s:%d 无转发记录", d.Domain, d.Port)
		// 记录到报告
		report.NoForwardDomains = append(report.NoForwardDomains, fmt.Sprintf("%s:%d", d.Domain, d.Port))
		return
	}

	// 按权重从大到小排序
	forwards := make([]models.ForwardRecord, len(d.Forwards))
	copy(forwards, d.Forwards)
	sort.Slice(forwards, func(i, j int) bool {
		if forwards[i].Weight != forwards[j].Weight {
			return forwards[i].Weight > forwards[j].Weight
		}
		return forwards[i].SortOrder < forwards[j].SortOrder
	})

	utils.Logger.Infof("🔄 转发池共 %d 个域名，开始按权重检测", len(forwards))

	var availableForward *models.ForwardRecord
	var resolvedIP string // 保存后端接口返回的实际 IP
	var bannedForwards []string

	// 检测每个转发域名
	for i, f := range forwards {
		// 调用进度回调
		progressCallback(i+1, len(forwards), f.ForwardDomain)

		// 跳过已封禁的转发域名
		if f.IsBan {
			utils.Logger.Infof("⏭️ 跳过已封禁的转发域名: %s (权重: %d)", f.ForwardDomain, f.Weight)
			continue
		}

		utils.Logger.Infof("🔍 [%d/%d] 检测转发域名: %s (权重: %d)", i+1, len(forwards), f.ForwardDomain, f.Weight)

		// 检测连通性（带连接进度）
		result, err := checkConnectivityWithProgress(f.ForwardDomain, d.Port, func(current int, total int) {
			// 调用进度回调显示连接进度
			progressCallback(current, total, fmt.Sprintf("正在检测第 %d/%d 次连接：%s", current, total, f.ForwardDomain))
		})
		if err != nil {
			utils.Logger.Warnf("⚠️ 转发域名 %s 检测失败: %v", f.ForwardDomain, err)
			banForward24Hours(&f)
			bannedForwards = append(bannedForwards, f.ForwardDomain)
			continue
		}

		// 检查检测结果
		if !result.Result {
			// 检查是否是因为连接超时导致的失败
			if strings.Contains(result.Message, "检测结束") && strings.Contains(result.Message, "无法连接") {
				utils.Logger.Warnf("❌ 转发域名 %s 5次连接测试全部失败，进行24小时封禁", f.ForwardDomain)
				banForward24Hours(&f)
				bannedForwards = append(bannedForwards, f.ForwardDomain)
				continue
			} else {
				// 其他原因导致的失败，不封禁
				utils.Logger.Warnf("⚠️ 转发域名 %s 检测失败，但不是因为5次连接全部失败: %s", f.ForwardDomain, result.Message)
			}
		} else {
			// 找到可用的转发域名
			utils.Logger.Infof("✅ 转发域名 %s 连通正常 (IP: %s)", f.ForwardDomain, result.TargetIp)
			availableForward = &f
			resolvedIP = result.TargetIp // 保存后端解析的实际 IP
			break
		}
	}

	// 通知封禁情况
	if len(bannedForwards) > 0 {
		report.BannedForwards = append(report.BannedForwards, bannedForwards...)
	}

	// 如果找到可用的转发域名，更新到 Cloudflare
	if availableForward != nil {
		updateToCloudflare(d, *availableForward, resolvedIP, report)
	} else {
		utils.Logger.Errorf("❌ 主域名 %s:%d 无可用转发域名", d.Domain, d.Port)
		// 记录到报告
		report.NoForwardDomains = append(report.NoForwardDomains, fmt.Sprintf("%s:%d", d.Domain, d.Port))
	}
}

// banForward24Hours 封禁转发域名24小时
func banForward24Hours(f *models.ForwardRecord) {
	f.IsBan = true
	f.BanTime = time.Now().Add(24 * time.Hour).Unix()
	f.ResolveStatus = "failed" // 标记为检测失败

	if err := operate.BanForward24Hours(db.DB, f); err != nil {
		utils.Logger.Errorf("❌ 封禁转发域名失败 %s: %v", f.ForwardDomain, err)
		return
	}

	utils.Logger.Infof("🚫 转发域名 %s 已封禁至 %s", f.ForwardDomain, time.Unix(f.BanTime, 0).Format("2006-01-02 15:04:05"))
}

// updateToCloudflare 更新 DNS 记录到 Cloudflare
func updateToCloudflare(d models.DomainRecord, f models.ForwardRecord, resolvedIP string, report *CheckReport) {
	if d.RecordId == "" {
		utils.Logger.Warnf("⚠️ 主域名 %s 没有 DNS ID，无法更新 Cloudflare", d.Domain)
		return
	}

	// 获取全局 Cloudflare 客户端
	client, err := cloudflare.GetGlobalClient()
	if err != nil {
		utils.Logger.Errorf("❌ 获取 Cloudflare 客户端失败: %v", err)
		return
	}

	// 根据记录类型确定更新内容（使用后端接口返回的 IP）
	var content string
	if f.RecordType == "A" {
		// A 记录使用后端解析的实际 IP
		content = resolvedIP
		utils.Logger.Infof("🔄 A 记录使用后端解析 IP: %s", resolvedIP)
	} else if f.RecordType == "CNAME" {
		// CNAME 记录使用转发域名作为内容，但仍需要保存解析的 IP
		content = f.ForwardDomain
		utils.Logger.Infof("🔄 CNAME 记录使用转发域名: %s", f.ForwardDomain)
	} else {
		utils.Logger.Warnf("⚠️ 不支持的记录类型: %s", f.RecordType)
		return
	}

	// 直接使用 DNS ID 更新
	updateErr := client.UpdateDNSRecordByID(
		d.Domain,                     // 域名（用于获取 Zone ID）
		d.ZoneId,                     // Zone ID
		d.RecordId,                   // DNS 记录 ID
		f.RecordType,                 // 记录类型
		d.Domain,                     // 记录名称
		content,                      // 记录内容
		config.Global.Cloudflare.TTL, // 使用配置文件中的 TTL
		false,                        // Proxied
	)

	if updateErr != nil {
		utils.Logger.Errorf("❌ 更新 Cloudflare 失败: %v", updateErr)
		return
	}

	// 更新成功，记录解析状态和 IP
	f.ResolveStatus = "success"
	f.LastResolvedAt = time.Now().Unix()
	// 更新数据库中的 IP（无论是 A 记录还是 CNAME 记录）
	f.IP = resolvedIP

	// 清除同一主域名下其他转发域名的 success 状态
	if err := operate.ClearOtherForwardStatus(db.DB, f.DomainRecordID, f.ID); err != nil {
		utils.Logger.Warnf("⚠️ 清除其他转发域名状态失败: %v", err)
	}

	if err := operate.UpdateForwardResolveStatus(db.DB, &f, "success", resolvedIP); err != nil {
		utils.Logger.Warnf("⚠️ 更新解析状态失败: %v", err)
	}

	// 记录到报告
	utils.Logger.Infof("✅ 已更新 Cloudflare: %s -> %s (%s)", d.Domain, f.ForwardDomain, f.RecordType)
	report.SwitchedDomains = append(report.SwitchedDomains, DomainSwitch{
		Domain:        d.Domain,
		Port:          d.Port,
		RecordType:    f.RecordType,
		NewRecord:     content,
		ForwardDomain: f.ForwardDomain,
		ISP:           f.ISP,
		Weight:        f.Weight,
	})
}

// incrementApiFailureCount 增加 API 失败计数
func incrementApiFailureCount() {
	apiFailureMutex.Lock()
	defer apiFailureMutex.Unlock()
	apiFailureCount++
	utils.Logger.Infof("⚠️ API 调用失败次数: %d/%d", apiFailureCount, getApiFailureThreshold())
}

// resetApiFailureCount 重置 API 失败计数
func resetApiFailureCount() {
	apiFailureMutex.Lock()
	defer apiFailureMutex.Unlock()
	if apiFailureCount > 0 {
		utils.Logger.Infof("✅ API 恢复正常，重置失败计数 (之前: %d 次失败)", apiFailureCount)
		apiFailureCount = 0
	}
}

// shouldSendApiFailureNotification 检查是否应该发送 API 失败通知
func shouldSendApiFailureNotification() bool {
	apiFailureMutex.Lock()
	defer apiFailureMutex.Unlock()
	return apiFailureCount >= getApiFailureThreshold()
}

// getApiFailureCount 获取当前 API 失败次数
func getApiFailureCount() int {
	apiFailureMutex.Lock()
	defer apiFailureMutex.Unlock()
	return apiFailureCount
}

// sendReport 发送检测报告汇总
func sendReport(bot *tgbotapi.BotAPI, report *CheckReport) {
	// 检查是否需要发送报告
	shouldSend := false

	// 如果有任何异常，发送报告
	if len(report.DisconnectedDomains) > 0 ||
		len(report.BannedForwards) > 0 ||
		len(report.SwitchedDomains) > 0 ||
		len(report.NoForwardDomains) > 0 {
		shouldSend = true
	}

	// 检查 API 失败次数是否超过阈值
	if len(report.FailedDomains) > 0 && shouldSendApiFailureNotification() {
		shouldSend = true
	}

	// 如果没有任何异常，不发送报告
	if !shouldSend {
		utils.Logger.Info("✅ 本次检测无异常，不发送报告")
		return
	}

	var message strings.Builder
	message.WriteString("📊 *自动检测报告*\n")
	message.WriteString(fmt.Sprintf("🕒 时间: `%s`\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// 1. DNS 切换成功
	if len(report.SwitchedDomains) > 0 {
		message.WriteString("✅ *DNS 自动切换成功*\n")
		for _, sw := range report.SwitchedDomains {
			message.WriteString(fmt.Sprintf(
				"  • `%s:%d`\n"+
					"    类型: `%s` | 运营商: `%s`\n"+
					"    转发: `%s` | 权重: `%d`\n",
				sw.Domain, sw.Port, sw.RecordType, sw.ISP, sw.ForwardDomain, sw.Weight,
			))
		}
		message.WriteString("\n")
	}

	// 2. 检测失败的主域名（只有当失败次数超过阈值时才发送）
	if len(report.FailedDomains) > 0 && shouldSendApiFailureNotification() {
		message.WriteString("⚠️ *接口调用失败*\n")
		for _, d := range report.FailedDomains {
			message.WriteString(fmt.Sprintf("  • `%s` (接口调用失败 %d 次)\n", d, getApiFailureCount()))
		}
		message.WriteString("\n")
	}

	// 3. 无法连通的主域名
	if len(report.DisconnectedDomains) > 0 {
		message.WriteString("🚨 *主域名连通性故障*\n")
		for _, d := range report.DisconnectedDomains {
			message.WriteString(fmt.Sprintf("  • `%s:%d` - %s\n", d.Domain, d.Port, d.Reason))
		}
		message.WriteString("\n")
	}

	// 4. 封禁的转发域名
	if len(report.BannedForwards) > 0 {
		message.WriteString("🚫 *转发域名已封禁 24小时*\n")
		for _, f := range report.BannedForwards {
			message.WriteString(fmt.Sprintf("  • `%s`\n", f))
		}
		message.WriteString("\n")
	}

	// 5. 无可用转发的主域名
	if len(report.NoForwardDomains) > 0 {
		message.WriteString("🆘 *无可用转发域名*\n")
		for _, d := range report.NoForwardDomains {
			message.WriteString(fmt.Sprintf("  • `%s` (请尽快处理！)\n", d))
		}
		message.WriteString("\n")
	}

	message.WriteString("──────────\n")
	message.WriteString("🔍 检测完成")

	// 发送汇总报告
	notifyAdmins(bot, message.String())
}

// notifyAdmins 发送通知给所有管理员
func notifyAdmins(bot *tgbotapi.BotAPI, message string) {
	// 获取所有管理员（已弃用缓存）
	var admins []models.TelegramAdmins
	if err := db.DB.Where("is_ban = ?", false).Find(&admins).Error; err != nil {
		utils.Logger.Errorf("❌ 从数据库获取管理员失败: %v", err)
		return
	}

	// 过滤未封禁的管理员（实际上上面的查询已经过滤了）
	var activeAdmins []models.TelegramAdmins
	for _, admin := range admins {
		if !admin.IsBan {
			activeAdmins = append(activeAdmins, admin)
		}
	}

	if len(activeAdmins) == 0 {
		utils.Logger.Warn("⚠️ 没有可用的管理员接收通知")
		return
	}

	utils.Logger.Infof("📢 向 %d 位管理员发送通知", len(activeAdmins))

	// 发送通知
	for _, admin := range activeAdmins {
		msg := tgbotapi.NewMessage(admin.UID, message)
		msg.ParseMode = "Markdown"
		if _, err := bot.Send(msg); err != nil {
			utils.Logger.Warnf("⚠️ 向管理员 %d 发送通知失败: %v", admin.UID, err)
		} else {
			utils.Logger.Infof("✅ 已向管理员 %d (%s) 发送通知", admin.UID, admin.Username)
		}
		// 防止频率限制
		time.Sleep(50 * time.Millisecond)
	}
}

// manualCheckHandler 手动检测命令处理器
func manualCheckHandler(ctx UpdateContext) {
	chatID := ctx.Update.Message.Chat.ID

	// 发送初始消息
	initMsg := tgbotapi.NewMessage(chatID, "🔍 *开始手动检测*\n\n正在初始化检测任务…")
	initMsg.ParseMode = "Markdown"
	sentMsg, err := ctx.Bot.Send(initMsg)
	if err != nil {
		utils.Logger.Errorf("发送初始消息失败: %v", err)
		return
	}
	messageID := sentMsg.MessageID

	// 异步执行手动检测，避免阻塞主线程
	go performManualCheck(ctx.Bot, chatID, messageID)
}

// performManualCheck 执行手动检测（带进度显示）
func performManualCheck(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	utils.Logger.Info("📊 开始执行手动检测任务...")

	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			utils.Logger.Errorf("❌ 数据库初始化失败: %v", err)
			edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("❌ 数据库初始化失败: %v", err))
			_, _ = bot.Send(edit)
			return
		}
	}

	// 创建检测报告
	report := &CheckReport{
		FailedDomains:       []string{},
		DisconnectedDomains: []DomainFailure{},
		BannedForwards:      []string{},
		SwitchedDomains:     []DomainSwitch{},
		NoForwardDomains:    []string{},
	}

	// 直接从数据库获取所有主域名
	var domains []models.DomainRecord
	if err := db.DB.Preload("Forwards", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("weight desc, sort_order asc, id asc")
	}).Order("sort_order asc, id asc").Find(&domains).Error; err != nil {
		utils.Logger.Errorf("❌ 获取主域名列表失败: %v", err)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fmt.Sprintf("❌ 获取主域名列表失败: %v", err))
		_, _ = bot.Send(edit)
		return
	}
	utils.Logger.Infof("✅ 从数据库读取到 %d 条主域名记录", len(domains))

	utils.Logger.Infof("📋 共获取到 %d 个主域名", len(domains))

	// 过滤未禁用的主域名
	var activeDomains []models.DomainRecord
	for _, d := range domains {
		if !d.IsDisableCheck {
			activeDomains = append(activeDomains, d)
		}
	}

	if len(activeDomains) == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "⚠️ 没有可检测的主域名（所有域名都已禁用检测）")
		_, _ = bot.Send(edit)
		return
	}

	// 更新进度：准备开始
	updateProgress(bot, chatID, messageID, fmt.Sprintf(
		"🔍 *手动检测进行中*\n\n"+
			"📋 总计: `%d` 个主域名\n"+
			"🔄 进度: `0/%d`\n\n"+
			"⏳ 正在准备...",
		len(activeDomains), len(activeDomains)))

	// 逐个检测主域名
	for i, d := range activeDomains {
		// 更新进度
		updateProgress(bot, chatID, messageID, fmt.Sprintf(
			"🔍 *手动检测进行中*\n\n"+
				"📋 总计: `%d` 个主域名\n"+
				"🔄 进度: `%d/%d`\n\n"+
				"🔍 正在检测: `%s:%d`\n"+
				"⏳ 请稍候...",
			len(activeDomains), i+1, len(activeDomains), d.Domain, d.Port))

		utils.Logger.Infof("🔍 检测主域名: %s:%d", d.Domain, d.Port)
		checkDomainWithProgress(d, report, func(current int, total int, forwardDomain string) {
			// 实时更新转发域名检测进度
			updateProgress(bot, chatID, messageID, fmt.Sprintf(
				"🔍 *手动检测进行中*\n\n"+
					"📋 总计: `%d` 个主域名\n"+
					"🔄 进度: `%d/%d`\n\n"+
					"🔍 正在检测: `%s:%d`\n"+
					"🔄 转发检测: `%s`\n"+
					"⏳ 请稍候...",
				len(activeDomains), i+1, len(activeDomains), d.Domain, d.Port, forwardDomain))
		})
	}

	// 检测完成，显示最终报告
	updateProgress(bot, chatID, messageID, fmt.Sprintf(
		"✅ *检测完成*\n\n"+
			"📋 总计: `%d` 个主域名\n"+
			"🔄 进度: `%d/%d`\n\n"+
			"📦 正在生成报告...",
		len(activeDomains), len(activeDomains), len(activeDomains)))

	// 等待 1 秒后显示最终报告
	time.Sleep(1 * time.Second)

	// 生成并显示报告
	sendManualCheckReport(bot, chatID, messageID, report)

	utils.Logger.Info("✅ 手动检测任务执行完毕")
}

// updateProgress 更新检测进度
func updateProgress(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "Markdown"
	if _, err := bot.Send(edit); err != nil {
		utils.Logger.Warnf("⚠️ 更新进度失败: %v", err)
	}
	// 防止频率限制
	time.Sleep(300 * time.Millisecond)
}

// sendManualCheckReport 发送手动检测报告
func sendManualCheckReport(bot *tgbotapi.BotAPI, chatID int64, messageID int, report *CheckReport) {
	// 如果没有任何异常，显示一切正常
	if len(report.FailedDomains) == 0 &&
		len(report.DisconnectedDomains) == 0 &&
		len(report.BannedForwards) == 0 &&
		len(report.SwitchedDomains) == 0 &&
		len(report.NoForwardDomains) == 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID,
			"✅ *检测完成*\n\n"+
				"🎉 所有主域名连通正常，未发现异常！")
		edit.ParseMode = "Markdown"
		_, _ = bot.Send(edit)
		return
	}

	var message strings.Builder
	message.WriteString("📊 *手动检测报告*\n")
	message.WriteString(fmt.Sprintf("🕒 时间: `%s`\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// 1. DNS 切换成功
	if len(report.SwitchedDomains) > 0 {
		message.WriteString("✅ *DNS 自动切换成功*\n")
		for _, sw := range report.SwitchedDomains {
			message.WriteString(fmt.Sprintf(
				"  • `%s:%d`\n"+
					"    类型: `%s` | 运营商: `%s`\n"+
					"    转发: `%s` | 权重: `%d`\n",
				sw.Domain, sw.Port, sw.RecordType, sw.ISP, sw.ForwardDomain, sw.Weight,
			))
		}
		message.WriteString("\n")
	}

	// 2. 检测失败的主域名
	if len(report.FailedDomains) > 0 {
		message.WriteString("⚠️ *检测失败*\n")
		for _, d := range report.FailedDomains {
			message.WriteString(fmt.Sprintf("  • `%s` (接口调用失败)\n", d))
		}
		message.WriteString("\n")
	}

	// 3. 无法连通的主域名
	if len(report.DisconnectedDomains) > 0 {
		message.WriteString("🚨 *主域名连通性故障*\n")
		for _, d := range report.DisconnectedDomains {
			message.WriteString(fmt.Sprintf("  • `%s:%d` - %s\n", d.Domain, d.Port, d.Reason))
		}
		message.WriteString("\n")
	}

	// 4. 封禁的转发域名
	if len(report.BannedForwards) > 0 {
		message.WriteString("🚫 *转发域名已封禁 24小时*\n")
		for _, f := range report.BannedForwards {
			message.WriteString(fmt.Sprintf("  • `%s`\n", f))
		}
		message.WriteString("\n")
	}

	// 5. 无可用转发的主域名
	if len(report.NoForwardDomains) > 0 {
		message.WriteString("🆘 *无可用转发域名*\n")
		for _, d := range report.NoForwardDomains {
			message.WriteString(fmt.Sprintf("  • `%s` (请尽快处理！)\n", d))
		}
		message.WriteString("\n")
	}

	message.WriteString("──────────\n")
	message.WriteString("🔍 检测完成")

	// 更新消息为最终报告
	edit := tgbotapi.NewEditMessageText(chatID, messageID, message.String())
	edit.ParseMode = "Markdown"
	_, _ = bot.Send(edit)
}
