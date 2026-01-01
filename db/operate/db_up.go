package operate

import (
	"fmt"
	"gorm.io/gorm"
	"telegram-auto-switch-dns-bot/db/models"
	"telegram-auto-switch-dns-bot/utils"
	"time"
)

// UpdateAdministrator updates an administrator in the database
func UpdateAdministrator(DB *gorm.DB, admin models.TelegramAdmins) error {
	// Update the administrator in the database
	if err := DB.Save(&admin).Error; err != nil {
		utils.Logger.Errorf("Database update failed: %v", err)
		return fmt.Errorf("database update failed: %w", err)
	}

	utils.Logger.Infof("✅ Administrator information updated in database UID=%d", admin.UID)
	return nil
}

// UpdateDomainRecord updates a domain record in the database
func UpdateDomainRecord(DB *gorm.DB, domain models.DomainRecord) error {
	if err := DB.Save(&domain).Error; err != nil {
		utils.Logger.Errorf("Failed to update domain record: %v", err)
		return fmt.Errorf("failed to update domain record: %w", err)
	}

	utils.Logger.Infof("✅ Domain record updated ID=%d", domain.ID)
	return nil
}

// UpdateForwardRecord updates a forward record in the database
func UpdateForwardRecord(DB *gorm.DB, forward models.ForwardRecord) error {
	if err := DB.Save(&forward).Error; err != nil {
		utils.Logger.Errorf("Failed to update forward record: %v", err)
		return fmt.Errorf("failed to update forward record: %w", err)
	}

	utils.Logger.Infof("✅ Forward record updated ID=%d", forward.ID)
	return nil
}

// BanForward24Hours bans a forward record for 24 hours
func BanForward24Hours(DB *gorm.DB, f *models.ForwardRecord) error {
	f.IsBan = true
	f.BanTime = time.Now().Add(24 * time.Hour).Unix()
	f.ResolveStatus = "failed" // Mark as detection failed

	if err := DB.Model(f).Updates(map[string]interface{}{
		"is_ban":         true,
		"ban_time":       f.BanTime,
		"resolve_status": "failed",
	}).Error; err != nil {
		utils.Logger.Errorf("❌ Failed to ban forward domain %s: %v", f.ForwardDomain, err)
		return err
	}

	utils.Logger.Infof("🚫 Forward domain %s banned until %s", f.ForwardDomain, time.Unix(f.BanTime, 0).Format("2006-01-02 15:04:05"))
	return nil
}

// UpdateForwardResolveStatus updates the resolve status of a forward record
func UpdateForwardResolveStatus(DB *gorm.DB, f *models.ForwardRecord, status, resolvedIP string) error {
	f.ResolveStatus = status
	f.LastResolvedAt = time.Now().Unix()
	// Update IP address in database
	f.IP = resolvedIP

	if err := DB.Model(f).Updates(map[string]interface{}{
		"resolve_status":   status,
		"last_resolved_at": f.LastResolvedAt,
		"ip":               resolvedIP, // Update IP address
	}).Error; err != nil {
		utils.Logger.Warnf("⚠️ Failed to update resolve status: %v", err)
		return err
	}

	return nil
}

// ClearOtherForwardStatus clears the success status of other forward records
func ClearOtherForwardStatus(DB *gorm.DB, domainRecordID, currentForwardID uint) error {
	// Clear success status of other forward domains
	if err := DB.Model(&models.ForwardRecord{}).Where(
		"domain_record_id = ? AND id != ?", domainRecordID, currentForwardID,
	).Updates(map[string]interface{}{
		"resolve_status": "never",
	}).Error; err != nil {
		utils.Logger.Warnf("⚠️ Failed to clear other forward domain status: %v", err)
		return err
	}

	return nil
}

// UpdateDomainRecordIfExists updates a domain record if it exists
func UpdateDomainRecordIfExists(DB *gorm.DB, domain *models.DomainRecord) error {
	var existingDomain models.DomainRecord
	err := DB.Where("domain = ? AND port = ?", domain.Domain, domain.Port).First(&existingDomain).Error

	if err == nil {
		// Exists → Update
		domain.ID = existingDomain.ID
		existingDomain.RecordId = domain.RecordId
		existingDomain.SortOrder = domain.SortOrder
		existingDomain.IsDisableCheck = domain.IsDisableCheck

		if err := DB.Save(&existingDomain).Error; err != nil {
			return fmt.Errorf("更新主域名失败: %w", err)
		}
	}

	return err
}

// UpdateForwardRecordIfExists updates a forward record if it exists
func UpdateForwardRecordIfExists(DB *gorm.DB, forward *models.ForwardRecord) error {
	// Check if already exists (within any port range of the same domain)
	var existingF models.ForwardRecord
	subQuery := DB.Model(&models.DomainRecord{}).Select("id").Where("domain = ?", forward.ForwardDomain)
	err := DB.Where(
		"forward_domain = ? AND domain_record_id IN (?)",
		forward.ForwardDomain, subQuery,
	).First(&existingF).Error

	if err == nil {
		// Already exists
		if existingF.DomainRecordID == forward.DomainRecordID {
			// Belongs to the current port domain → Update
			forward.ID = existingF.ID
			if err := DB.Save(forward).Error; err != nil {
				return fmt.Errorf("更新子域名失败: %w", err)
			}
		}
		// Same domain other ports already exist → Skip adding
	}

	return err
}

// AutoUnbanForward automatically unbans a forward record if ban time has expired
func AutoUnbanForward(DB *gorm.DB, f *models.ForwardRecord) error {
	f.IsBan = false
	if err := DB.Model(f).Updates(map[string]interface{}{"is_ban": false}).Error; err != nil {
		utils.Logger.Errorf("❌ Failed to automatically unban forward record: %v", err)
		return err
	}

	utils.Logger.Infof("✅ Automatically unbanned forward record ID=%d", f.ID)
	return nil
}

// AddForwardRecord adds a new forward record to a domain
func AddForwardRecord(DB *gorm.DB, forward models.ForwardRecord) error {
	// Verify that the domain record exists
	var domain models.DomainRecord
	if err := DB.Where("id = ?", forward.DomainRecordID).First(&domain).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("domain record with ID %d does not exist", forward.DomainRecordID)
		}
		return fmt.Errorf("failed to verify domain record: %w", err)
	}

	// Set timestamps
	now := time.Now().Unix()
	forward.CreatedAt = now
	forward.UpdatedAt = now

	// Set default values if not provided
	if forward.RecordType == "" {
		forward.RecordType = "A"
	}
	if forward.ResolveStatus == "" {
		forward.ResolveStatus = "never"
	}

	// Create the forward record
	if err := DB.Create(&forward).Error; err != nil {
		utils.Logger.Errorf("Failed to create forward record: %v", err)
		return fmt.Errorf("failed to create forward record: %w", err)
	}

	utils.Logger.Infof("✅ Forward record created ID=%d for domain ID=%d", forward.ID, forward.DomainRecordID)
	return nil
}
