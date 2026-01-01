package cloudflare

import (
	"context"
	"fmt"
	"github.com/cloudflare/cloudflare-go"
	"strings"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/utils"
)

// Client Cloudflare 客户端封装
type Client struct {
	api    *cloudflare.API
	zoneID string
}

// 全局客户端实例
var globalClient *Client

// InitGlobalClient 初始化全局 Cloudflare 客户端
func InitGlobalClient() error {
	apiToken := config.Global.Cloudflare.ApiToken
	if apiToken == "" {
		return fmt.Errorf("cloudflare API Token 未配置")
	}

	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		return fmt.Errorf("创建 Cloudflare 客户端失败: %w", err)
	}

	globalClient = &Client{
		api:    api,
		zoneID: "", // Zone ID 将在需要时动态获取
	}

	utils.Logger.Info("✅ Cloudflare 全局客户端初始化成功")
	return nil
}

// GetGlobalClient 获取全局客户端
func GetGlobalClient() (*Client, error) {
	if globalClient == nil {
		if err := InitGlobalClient(); err != nil {
			return nil, err
		}
	}
	return globalClient, nil
}

// NewClientByDomain 通过域名创建 Cloudflare 客户端（自动查找 Zone ID）
func NewClientByDomain(domain string) (*Client, error) {
	cfg := config.Global.Cloudflare
	api, err := cloudflare.NewWithAPIToken(cfg.ApiToken)
	if err != nil {
		return nil, fmt.Errorf("创建 Cloudflare 客户端失败: %w", err)
	}

	// 查找 Zone ID
	zoneID, err := api.ZoneIDByName(domain)
	if err != nil {
		return nil, fmt.Errorf("查找域名 %s 的 Zone ID 失败: %w", domain, err)
	}

	utils.Logger.Infof("✅ 找到域名 %s 的 Zone ID: %s", domain, zoneID)

	return &Client{
		api:    api,
		zoneID: zoneID,
	}, nil
}

// GetZoneIDByDomain 根据域名查找 Zone ID（静态方法）
func GetZoneIDByDomain(apiToken string, domain string) (string, error) {
	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		return "", fmt.Errorf("创建 Cloudflare 客户端失败: %w", err)
	}

	zoneID, err := api.ZoneIDByName(domain)
	if err != nil {
		return "", fmt.Errorf("查找域名 %s 的 Zone ID 失败: %w", domain, err)
	}

	return zoneID, nil
}

// CreateARecord 创建 A 记录
func (c *Client) CreateARecord(ctx context.Context, name string, ip string, ttl int, proxied bool) (*cloudflare.DNSRecord, error) {
	record := cloudflare.CreateDNSRecordParams{
		Type:    "A",
		Name:    name,
		Content: ip,
		TTL:     ttl,
		Proxied: &proxied,
	}

	resp, err := c.api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(c.zoneID), record)
	if err != nil {
		return nil, fmt.Errorf("创建 A 记录失败: %w", err)
	}

	utils.Logger.Infof("✅ 已创建 A 记录: %s -> %s (ID: %s)", name, ip, resp.ID)
	return &resp, nil
}

// CreateCNAMERecord 创建 CNAME 记录
func (c *Client) CreateCNAMERecord(ctx context.Context, name string, target string, ttl int, proxied bool) (*cloudflare.DNSRecord, error) {
	record := cloudflare.CreateDNSRecordParams{
		Type:    "CNAME",
		Name:    name,
		Content: target,
		TTL:     ttl,
		Proxied: &proxied,
	}

	resp, err := c.api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(c.zoneID), record)
	if err != nil {
		return nil, fmt.Errorf("创建 CNAME 记录失败: %w", err)
	}

	utils.Logger.Infof("✅ 已创建 CNAME 记录: %s -> %s (ID: %s)", name, target, resp.ID)
	return &resp, nil
}

// UpdateARecord 更新 A 记录
func (c *Client) UpdateARecord(ctx context.Context, recordID string, name string, ip string, proxied bool) (*cloudflare.DNSRecord, error) {
	record := cloudflare.UpdateDNSRecordParams{
		ID:      recordID,
		Type:    "A",
		Name:    name,
		Content: ip,
		TTL:     config.Global.Cloudflare.TTL,
		Proxied: &proxied,
	}

	resp, err := c.api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(c.zoneID), record)
	if err != nil {
		return nil, fmt.Errorf("更新 A 记录失败: %w", err)
	}

	utils.Logger.Infof("✅ 已更新 A 记录: %s -> %s (ID: %s)", name, ip, recordID)
	return &resp, nil
}

// UpdateCNAMERecord 更新 CNAME 记录
func (c *Client) UpdateCNAMERecord(ctx context.Context, recordID string, name string, target string, proxied bool) (*cloudflare.DNSRecord, error) {
	record := cloudflare.UpdateDNSRecordParams{
		ID:      recordID,
		Type:    "CNAME",
		Name:    name,
		Content: target,
		TTL:     config.Global.Cloudflare.TTL,
		Proxied: &proxied,
	}

	resp, err := c.api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(c.zoneID), record)
	if err != nil {
		return nil, fmt.Errorf("更新 CNAME 记录失败: %w", err)
	}

	utils.Logger.Infof("✅ 已更新 CNAME 记录: %s -> %s (ID: %s)", name, target, recordID)
	return &resp, nil
}

// DeleteDNSRecord 删除 DNS 记录
func (c *Client) DeleteDNSRecord(ctx context.Context, recordID string) error {
	err := c.api.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(c.zoneID), recordID)
	if err != nil {
		return fmt.Errorf("删除 DNS 记录失败: %w", err)
	}

	utils.Logger.Infof("✅ 已删除 DNS 记录: %s", recordID)
	return nil
}

// ListDNSRecords 列出所有 DNS 记录
func (c *Client) ListDNSRecords(ctx context.Context, recordType string) ([]cloudflare.DNSRecord, error) {
	params := cloudflare.ListDNSRecordsParams{}
	if recordType != "" {
		params.Type = recordType
	}

	records, _, err := c.api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return nil, fmt.Errorf("列出 DNS 记录失败: %w", err)
	}

	return records, nil
}

// GetDNSRecordByName 根据域名查找 DNS 记录
func (c *Client) GetDNSRecordByName(ctx context.Context, name string, recordType string) (*cloudflare.DNSRecord, error) {
	params := cloudflare.ListDNSRecordsParams{
		Name: name,
	}
	if recordType != "" {
		params.Type = recordType
	}

	records, _, err := c.api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return nil, fmt.Errorf("查询 DNS 记录失败: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("未找到记录: %s", name)
	}

	return &records[0], nil
}

// GetZoneID 返回客户端的 Zone ID
func (c *Client) GetZoneID() string {
	return c.zoneID
}

// UpdateDNSRecordByID 通过 DNS 记录 ID 直接更新（使用全局客户端）
// 如果 zoneId 为空，则通过域名提取根域名并查询 Zone ID
func (c *Client) UpdateDNSRecordByID(domain string, zoneId string, recordID string, recordType string, name string, content string, ttl int, proxied bool) error {
	var zoneID string
	var err error

	// 如果提供了 zoneId，直接使用；否则通过域名提取根域名并查询 Zone ID
	if zoneId != "" {
		zoneID = zoneId
	} else {
		// 提取根域名（取后两部分）
		rootDomain := extractRootDomain(domain)
		utils.Logger.Infof("🔍 从 %s 提取根域名: %s", domain, rootDomain)

		// 通过根域名获取 Zone ID
		zoneID, err = c.api.ZoneIDByName(rootDomain)
		if err != nil {
			return fmt.Errorf("查找域名 %s 的 Zone ID 失败: %w", rootDomain, err)
		}
	}

	record := cloudflare.UpdateDNSRecordParams{
		ID:      recordID,
		Type:    recordType,
		Name:    name,
		Content: content,
		TTL:     ttl,
		Proxied: &proxied,
	}

	_, err = c.api.UpdateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), record)
	if err != nil {
		return fmt.Errorf("更新 DNS 记录失败: %w", err)
	}

	utils.Logger.Infof("✅ 已更新 DNS 记录: %s -> %s (ID: %s, Type: %s, TTL: %d)", name, content, recordID, recordType, ttl)
	return nil
}

// extractRootDomain 提取根域名（取后两部分）
func extractRootDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return domain
}
