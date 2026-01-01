package models

import (
	"gorm.io/gorm"
	"time"
)

func (d *DomainRecord) BeforeCreate(*gorm.DB) (err error) {
	now := time.Now().Unix()
	d.CreatedAt = now
	d.UpdatedAt = now
	return nil
}

func (d *DomainRecord) BeforeUpdate(*gorm.DB) (err error) {
	d.UpdatedAt = time.Now().Unix()
	return nil
}

// BeforeCreate 时间自动处理
func (f *ForwardRecord) BeforeCreate(*gorm.DB) (err error) {
	now := time.Now().Unix()
	f.CreatedAt = now
	f.UpdatedAt = now
	return nil
}

func (f *ForwardRecord) BeforeUpdate(*gorm.DB) (err error) {
	f.UpdatedAt = time.Now().Unix()
	return nil
}

// BeforeCreate 时间自动处理
func (t *TelegramAdmins) BeforeCreate(*gorm.DB) (err error) {
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	return nil
}

func (t *TelegramAdmins) BeforeUpdate(*gorm.DB) (err error) {
	t.UpdatedAt = time.Now().Unix()
	return nil
}
