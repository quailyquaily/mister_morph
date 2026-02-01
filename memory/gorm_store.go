package memory

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/quailyquaily/mister_morph/db/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GormStore struct {
	DB *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{DB: db}
}

func (s *GormStore) Get(ctx context.Context, subjectID, namespace, key string, opt ReadOptions) (Item, bool, error) {
	if s == nil || s.DB == nil {
		return Item{}, false, nil
	}
	subjectID = strings.TrimSpace(subjectID)
	namespace = strings.TrimSpace(namespace)
	key = strings.TrimSpace(key)
	if subjectID == "" || namespace == "" || key == "" {
		return Item{}, false, nil
	}

	q := s.DB.WithContext(ctx).Model(&models.MemoryItem{}).
		Where("subject_id = ? AND namespace = ? AND key = ?", subjectID, namespace, key)
	q = applyVisibilityFilter(q, opt.Context)

	var row models.MemoryItem
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Item{}, false, nil
		}
		return Item{}, false, err
	}
	return modelToItem(row), true, nil
}

func (s *GormStore) List(ctx context.Context, subjectID, namespace string, opt ReadOptions) ([]Item, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	subjectID = strings.TrimSpace(subjectID)
	namespace = strings.TrimSpace(namespace)
	if subjectID == "" || namespace == "" {
		return nil, nil
	}

	limit := opt.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := s.DB.WithContext(ctx).Model(&models.MemoryItem{}).
		Where("subject_id = ? AND namespace = ?", subjectID, namespace).
		Order("updated_at DESC").
		Limit(limit)
	q = applyVisibilityFilter(q, opt.Context)

	prefix := strings.TrimSpace(opt.Prefix)
	if prefix != "" {
		q = q.Where("key LIKE ?", prefix+"%")
	}

	var rows []models.MemoryItem
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, modelToItem(r))
	}
	return out, nil
}

func (s *GormStore) Put(ctx context.Context, subjectID, namespace, key, value string, opt PutOptions) (Item, error) {
	if s == nil || s.DB == nil {
		return Item{}, nil
	}
	subjectID = strings.TrimSpace(subjectID)
	namespace = strings.TrimSpace(namespace)
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if subjectID == "" || namespace == "" || key == "" {
		return Item{}, nil
	}

	vis := PrivateOnly
	if opt.Visibility != nil {
		vis = *opt.Visibility
	}
	if vis != PublicOK && vis != PrivateOnly {
		vis = PrivateOnly
	}

	now := time.Now().Unix()
	row := models.MemoryItem{
		SubjectID:  subjectID,
		Namespace:  namespace,
		Key:        key,
		Value:      value,
		Visibility: int(vis),
		Confidence: opt.Confidence,
		Source:     opt.Source,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "subject_id"},
				{Name: "namespace"},
				{Name: "key"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"value":      row.Value,
				"visibility": row.Visibility,
				"confidence": row.Confidence,
				"source":     row.Source,
				"updated_at": now,
			}),
		}).
		Create(&row).Error
	if err != nil {
		return Item{}, err
	}

	// Avoid an immediate read-back: in public contexts private_only must not be readable at all.
	// Return best-effort timestamps (created_at may differ if this was an update).
	return modelToItem(row), nil
}

func (s *GormStore) Delete(ctx context.Context, subjectID, namespace, key string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	subjectID = strings.TrimSpace(subjectID)
	namespace = strings.TrimSpace(namespace)
	key = strings.TrimSpace(key)
	if subjectID == "" || namespace == "" || key == "" {
		return nil
	}
	return s.DB.WithContext(ctx).
		Where("subject_id = ? AND namespace = ? AND key = ?", subjectID, namespace, key).
		Delete(&models.MemoryItem{}).Error
}

func (s *GormStore) DeleteNamespace(ctx context.Context, subjectID, namespace string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	subjectID = strings.TrimSpace(subjectID)
	namespace = strings.TrimSpace(namespace)
	if subjectID == "" || namespace == "" {
		return nil
	}
	return s.DB.WithContext(ctx).
		Where("subject_id = ? AND namespace = ?", subjectID, namespace).
		Delete(&models.MemoryItem{}).Error
}

func (s *GormStore) WipeSubject(ctx context.Context, subjectID string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil
	}
	return s.DB.WithContext(ctx).
		Where("subject_id = ?", subjectID).
		Delete(&models.MemoryItem{}).Error
}

func applyVisibilityFilter(q *gorm.DB, c RequestContext) *gorm.DB {
	switch c {
	case ContextPrivate:
		return q
	case ContextPublic, ContextUnknown, "":
		return q.Where("visibility = ?", int(PublicOK))
	default:
		return q.Where("visibility = ?", int(PublicOK))
	}
}

func modelToItem(m models.MemoryItem) Item {
	return Item{
		SubjectID:  m.SubjectID,
		Namespace:  m.Namespace,
		Key:        m.Key,
		Value:      m.Value,
		Visibility: Visibility(m.Visibility),
		Confidence: m.Confidence,
		Source:     m.Source,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}
