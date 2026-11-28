package models

// DocumentPassword contains the current reading password hash and unlock generation.
// A document without a row has no reading password.
type DocumentPassword struct {
	DocumentID   string `gorm:"column:document_id;primaryKey"`
	PasswordHash string `gorm:"column:password_hash" json:"-"`
	Generation   string `gorm:"column:generation" json:"-"`
}

func (DocumentPassword) TableName() string {
	return "document_passwords"
}

var DocumentPasswordColumns = struct {
	DocumentID   string
	PasswordHash string
	Generation   string
}{
	DocumentID:   "document_id",
	PasswordHash: "password_hash",
	Generation:   "generation",
}
