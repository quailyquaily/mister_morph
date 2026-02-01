package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mister_morph/db/models"
	"gorm.io/gorm"
)

type Identity struct {
	Enabled     bool
	ExternalKey string
	SubjectID   string
}

type IdentityResolver interface {
	ResolveTelegram(ctx context.Context, userID int64) (Identity, error)
}

type Resolver struct {
	DB *gorm.DB
}

func (r *Resolver) ResolveTelegram(ctx context.Context, userID int64) (Identity, error) {
	if userID <= 0 {
		return Identity{Enabled: false}, nil
	}
	ext := fmt.Sprintf("telegram:%d", userID)
	subject := "ext:" + ext

	if r == nil || r.DB == nil {
		return Identity{Enabled: true, ExternalKey: ext, SubjectID: subject}, nil
	}

	var row models.IdentityLink
	err := r.DB.WithContext(ctx).
		Model(&models.IdentityLink{}).
		Where("external_key = ?", ext).
		First(&row).Error
	if err == nil && row.SubjectID != "" {
		subject = strings.TrimSpace(row.SubjectID)
	}
	// If not found (or any error), fall back to ext:*.
	return Identity{Enabled: true, ExternalKey: ext, SubjectID: subject}, nil
}
