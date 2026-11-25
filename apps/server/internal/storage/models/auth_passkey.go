package models

import "time"

// AuthPasskey stores the sealed credential and authenticator state for one enrollment.
type AuthPasskey struct {
	PasskeyID           string     `gorm:"column:passkey_id;primaryKey"`
	CredentialIDHash    string     `gorm:"column:credential_id_hash" json:"-"`
	UserID              string     `gorm:"column:user_id"`
	Name                string     `gorm:"column:name"`
	EncryptedCredential string     `gorm:"column:encrypted_credential" json:"-"`
	MFAGeneration       int64      `gorm:"column:mfa_generation" json:"-"`
	Version             int64      `gorm:"column:version" json:"-"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	LastUsedAt          *time.Time `gorm:"column:last_used_at"`
}

func (AuthPasskey) TableName() string { return "auth_passkeys" }

var AuthPasskeyColumns = struct {
	PasskeyID           string
	CredentialIDHash    string
	UserID              string
	Name                string
	EncryptedCredential string
	MFAGeneration       string
	Version             string
	CreatedAt           string
	LastUsedAt          string
}{
	PasskeyID:           "passkey_id",
	CredentialIDHash:    "credential_id_hash",
	UserID:              "user_id",
	Name:                "name",
	EncryptedCredential: "encrypted_credential",
	MFAGeneration:       "mfa_generation",
	Version:             "version",
	CreatedAt:           "created_at",
	LastUsedAt:          "last_used_at",
}

// AuthPasskeyChallenge stores short-lived WebAuthn ceremony state.
// UserID is empty for a discoverable login. ExpiresAt is a Unix timestamp in seconds.
type AuthPasskeyChallenge struct {
	TokenHash   string `gorm:"column:token_hash;primaryKey" json:"-"`
	UserID      string `gorm:"column:user_id"`
	Purpose     string `gorm:"column:purpose"`
	SessionJSON string `gorm:"column:session_json" json:"-"`
	ExpiresAt   int64  `gorm:"column:expires_at"`
}

func (AuthPasskeyChallenge) TableName() string { return "auth_passkey_challenges" }

var AuthPasskeyChallengeColumns = struct {
	TokenHash   string
	UserID      string
	Purpose     string
	SessionJSON string
	ExpiresAt   string
}{
	TokenHash:   "token_hash",
	UserID:      "user_id",
	Purpose:     "purpose",
	SessionJSON: "session_json",
	ExpiresAt:   "expires_at",
}
