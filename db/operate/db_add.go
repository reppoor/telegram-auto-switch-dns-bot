package operate

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"strings"
	"telegram-auto-switch-dns-bot/cloudflare"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/utils"
	"time"
)

// extractRootDomain 提取根域名（取后两部分）
// 例：www.example.com -> example.com
func extractRootDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		// 返回后两部分
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return domain
}

// SaveToDBOnly 仅保存到数据库（已弃用缓存）
func SaveToDBOnly(DB *gorm.DB, jsonStr string) error {
	var domains []models.DomainRecord

	if err := json.Unmarshal([]byte(jsonStr), &domains); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}

	for _, d := range domains {
		// 提取根域名用于创建 Cloudflare 客户端
		// 如果域名只有两部分（如 example.com），直接使用；否则提取根域名
		rootDomain := extractRootDomain(d.Domain)
		utils.Logger.Infof("📌 域名: %s, 根域名: %s", d.Domain, rootDomain)

		// 使用根域名创建 Cloudflare 客户端
		cfClient, err := cloudflare.NewClientByDomain(rootDomain)
		if err != nil {
			return fmt.Errorf("无法连接 Cloudflare (根域名: %s): %w", rootDomain, err)
		}

		// 检查域名在 Cloudflare 中是否存在对应的 DNS 记录
		ctx := context.Background()
		dnsRecord, err := cfClient.GetDNSRecordByName(ctx, d.Domain, "")
		if err != nil {
			utils.Logger.Warnf("⚠️ 未在 Cloudflare 中找到域名: %s, 错误: %v", d.Domain, err)
			// 不返回错误，继续处理（DNS ID 为空）
		} else {
			// 设置 DNS ID 和 Zone ID
			d.RecordId = dnsRecord.ID
			d.ZoneId = cfClient.GetZoneID() // 使用客户端的 GetZoneID 方法
			utils.Logger.Infof("✅ 自动获取 DNS ID：%s -> %s (类型: %s, 内容: %s)", d.Domain, dnsRecord.ID, dnsRecord.Type, dnsRecord.Content)
			utils.Logger.Infof("✅ 自动获取 Zone ID：%s -> %s", d.Domain, cfClient.GetZoneID())
		}

		// 查找主域名是否存在
		err = UpdateDomainRecordIfExists(DB, &d)
		if err == gorm.ErrRecordNotFound {
			// Does not exist → Create will automatically trigger Hook to set CreatedAt / UpdatedAt
			if err := DB.Create(&d).Error; err != nil {
				return fmt.Errorf("创建主域名失败: %w", err)
			}
		} else if err != nil {
			return err
		}

		// 遍历子域名 forwards
		for _, f := range d.Forwards {
			f.DomainRecordID = d.ID // Now d.ID is set after creating/updating the domain

			if f.ID == 0 {
				// 检查转发域名是否已存在，避免重复添加
				var existingForward models.ForwardRecord
				err := DB.Where("domain_record_id = ? AND forward_domain = ?", f.DomainRecordID, f.ForwardDomain).First(&existingForward).Error
				if err == nil {
					// 转发域名已存在，跳过添加
					utils.Logger.Infof("⚠️ 转发域名已存在，跳过添加: %s", f.ForwardDomain)
					continue
				} else if err != gorm.ErrRecordNotFound {
					// 其他错误
					return fmt.Errorf("查询转发域名时出错: %w", err)
				}

				// 转发域名不存在，创建新记录
				if err := DB.Create(&f).Error; err != nil {
					return fmt.Errorf("创建子域名失败: %w", err)
				}
			} else {
				// Directly update (automatically update time)
				if err := UpdateForwardRecord(DB, f); err != nil {
					return fmt.Errorf("更新子域名失败: %w", err)
				}
			}
		}

	}

	return nil
}

func AddAdministrator(DB *gorm.DB, admin models.TelegramAdmins) error {
	// 1️⃣ 检查数据库是否已存在该 UID
	var existing models.TelegramAdmins
	err := DB.Where("uid = ?", admin.UID).First(&existing).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 不存在 → 新增
			now := time.Now().Unix()
			admin.CreatedAt = now
			admin.UpdatedAt = now

			if err := DB.Create(&admin).Error; err != nil {
				utils.Logger.Errorf("数据库写入失败: %v", err)
				return fmt.Errorf("数据库写入失败: %w", err)
			}
			utils.Logger.Infof("✅ 新管理员已写入数据库 UID=%d", admin.UID)
		} else {
			// 查询报错
			utils.Logger.Errorf("数据库查询失败: %v", err)
			return fmt.Errorf("数据库查询失败: %w", err)
		}
	} else {
		// 已存在 → 不写入，直接返回
		utils.Logger.Infof("✅ 管理员已存在，跳过写入 UID=%d", admin.UID)
		// 使用现有数据（已弃用缓存）
		admin = existing
	}

	utils.Logger.Infof("✅ 管理员信息已写入数据库 UID=%d", admin.UID)
	return nil
}
