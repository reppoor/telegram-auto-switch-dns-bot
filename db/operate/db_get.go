package operate

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/utils"
)

var ErrAdminNotFound = errors.New("administrator not found")

func GetAdministrator(DB *gorm.DB, uid int64) (*models.TelegramAdmins, error) {
	admin := &models.TelegramAdmins{}

	// 直接从数据库查询
	utils.Logger.Infof("[Admin] 从数据库查询 UID=%d", uid)
	err := DB.Where("uid = ?", uid).First(admin).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("管理员 UID=%d 不存在: %w", uid, ErrAdminNotFound)
		}
		return nil, fmt.Errorf("数据库查询失败: %w", err)
	}

	utils.Logger.Infof("[Admin] ✅ 从数据库获取管理员 UID=%d", uid)
	return admin, nil
}
