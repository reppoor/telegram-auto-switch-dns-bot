package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"telegram-auto-switch-dns-bot/config"
	"telegram-auto-switch-dns-bot/db"
	"telegram-auto-switch-dns-bot/db/operate"
	"telegram-auto-switch-dns-bot/utils"
)

// IsSuperAdmin 检查是否为超管（来自 conf.yaml）
func IsSuperAdmin(userID int64) bool {
	return userID == config.Global.Telegram.Id
}

// IsAdminAndNotBanned 检查用户是否为有效管理员（未被封禁）
// 返回 (isAdmin, isBanned, error)
func IsAdminAndNotBanned(userID int64) (bool, bool, error) {
	// 超管永远不被封禁
	if IsSuperAdmin(userID) {
		return true, false, nil
	}

	// 初始化数据库
	if db.DB == nil {
		if err := db.InitDB(); err != nil {
			return false, false, err
		}
	}

	// 查询管理员信息
	admin, err := operate.GetAdministrator(db.DB, userID)
	if err != nil {
		// 不是管理员
		return false, false, err
	}

	// 是管理员，检查封禁状态
	return true, admin.IsBan, nil
}

// CanManageAdmins 检查用户是否可以管理管理员列表（仅超管）
func CanManageAdmins(userID int64) bool {
	return IsSuperAdmin(userID)
}

// ValidateBackendKey 验证后端通信密钥
func ValidateBackendKey(key string, c *gin.Context) bool {
	if key != config.Global.BackendListen.Key {
		utils.Logger.Error("后端通信密钥验证失败")
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "密钥验证失败"})
		return false
	}
	return true
}
