package models

import "time"

// User 对应 users 表。
type User struct {
	ID                   int64        `gorm:"column:id;primaryKey;autoIncrement"`
	UserID               string       `gorm:"column:user_id"`
	Email                string       `gorm:"column:email"`
	PasswordHash         string       `gorm:"column:password_hash"`
	TOTPSecret           string       `gorm:"column:totp_secret;default:''" json:"-"`
	TOTPLastStep         int64        `gorm:"column:totp_last_step;default:-1" json:"-"`
	MFAGeneration        int64        `gorm:"column:mfa_generation;default:0" json:"-"`
	Name                 string       `gorm:"column:name"`
	AvatarURL            string       `gorm:"column:avatar_url"`
	Status               EntityStatus `gorm:"column:status"`
	BannedReason         string       `gorm:"column:banned_reason"`
	BannedAt             *time.Time   `gorm:"column:banned_at"`
	DeletedAt            *time.Time   `gorm:"column:deleted_at"`
	LoginIPAddressesJSON string       `gorm:"column:login_ip_addresses_json" json:"-"`
	CreatedAt            time.Time    `gorm:"column:created_at"`
	UpdatedAt            time.Time    `gorm:"column:updated_at"`
}

func (User) TableName() string {
	return "users"
}

var UserColumns = struct {
	ID                   string
	UserID               string
	Email                string
	PasswordHash         string
	TOTPSecret           string
	TOTPLastStep         string
	MFAGeneration        string
	Name                 string
	AvatarURL            string
	Status               string
	BannedReason         string
	BannedAt             string
	DeletedAt            string
	LoginIPAddressesJSON string
	CreatedAt            string
	UpdatedAt            string
}{
	ID:                   "id",
	UserID:               "user_id",
	Email:                "email",
	PasswordHash:         "password_hash",
	TOTPSecret:           "totp_secret",
	TOTPLastStep:         "totp_last_step",
	MFAGeneration:        "mfa_generation",
	Name:                 "name",
	AvatarURL:            "avatar_url",
	Status:               "status",
	BannedReason:         "banned_reason",
	BannedAt:             "banned_at",
	DeletedAt:            "deleted_at",
	LoginIPAddressesJSON: "login_ip_addresses_json",
	CreatedAt:            "created_at",
	UpdatedAt:            "updated_at",
}
