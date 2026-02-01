package models

type IdentityLink struct {
	ExternalKey string `gorm:"column:external_key;type:text;primaryKey"`
	SubjectID   string `gorm:"column:subject_id;type:text;not null;index:idx_identity_subject"`
	CreatedAt   int64  `gorm:"column:created_at;not null"`
	UpdatedAt   int64  `gorm:"column:updated_at;not null"`
}

func (IdentityLink) TableName() string { return "identity_links" }
