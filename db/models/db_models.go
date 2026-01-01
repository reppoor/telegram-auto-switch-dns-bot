package models

// ForwardRecord 表示单个转发域名的元信息
type ForwardRecord struct {
	ID             uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	DomainRecordID uint   `gorm:"not null;index" json:"domain_record_id"`        // 外键 -> DomainRecord.ID
	ForwardDomain  string `gorm:"size:255;not null;index" json:"forward_domain"` // 子域名，如 er.sdj.com
	IP             string `gorm:"size:64;not null" json:"ip"`                    // 解析 IP
	ISP            string `gorm:"size:64" json:"isp"`                            // 运营商，可选
	IsBan          bool   `gorm:"default:false" json:"is_ban"`                   // 是否封禁
	BanTime        int64  `json:"ban_time,omitempty"`                            // 封禁时间（可空）
	Weight         int    `gorm:"default:0"`                                     // 权重，默认值为0
	SortOrder      int    `gorm:"default:0" json:"sort_order"`                   // 排序字段
	RecordType     string `gorm:"size:16;default:'A'" json:"record_type"`
	LastResolvedAt int64  `gorm:"default:0" json:"last_resolved_at"`             // 最后解析时间戳
	ResolveStatus  string `gorm:"size:32;default:'never'" json:"resolve_status"` // 解析状态: never, success, failed
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

// DomainRecord 表示主域名记录
type DomainRecord struct {
	ID             uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	Domain         string          `gorm:"size:255;not null;uniqueIndex:idx_domain_port" json:"domain"`                             // 主域名，如 main.jkl.com
	Port           int             `gorm:"default:80;uniqueIndex:idx_domain_port" json:"port"`                                      // 对应端口
	RecordId       string          `gorm:"size:255" json:"record_id"`                                                               // Cloudflare DNS 记录 ID
	ZoneId         string          `gorm:"size:255" json:"zone_id"`                                                                 // Cloudflare Zone ID
	Forwards       []ForwardRecord `gorm:"foreignKey:DomainRecordID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"forwards"` // 一对多关联
	IsDisableCheck bool            `gorm:"default:false" json:"is_disable_check"`
	SortOrder      int             `gorm:"default:0" json:"sort_order"` // 排序字段
	CreatedAt      int64           `json:"created_at"`
	UpdatedAt      int64           `json:"updated_at"`
}

type TelegramAdmins struct {
	ID        int64  `gorm:"primaryKey;column:id"`
	UID       int64  `gorm:"column:uid;not null;index"`
	Username  string `gorm:"column:user_name"`
	LastName  string `gorm:"column:last_name"`
	FirstName string `gorm:"column:first_name"`
	Role      string `gorm:"column:role"` // admin, super, viewer...
	Remark    string `gorm:"column:remark"`
	AddedBy   int64  `gorm:"column:added_by"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	IsBan     bool   `gorm:"default:false"`
}
