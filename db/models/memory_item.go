package models

type MemoryItem struct {
	SubjectID  string   `gorm:"column:subject_id;type:text;not null;uniqueIndex:uniq_subject_ns_key,priority:1;index:idx_subject_ns_vis_updated,priority:1"`
	Namespace  string   `gorm:"column:namespace;type:text;not null;uniqueIndex:uniq_subject_ns_key,priority:2;index:idx_subject_ns_vis_updated,priority:2"`
	Key        string   `gorm:"column:key;type:text;not null;uniqueIndex:uniq_subject_ns_key,priority:3"`
	Value      string   `gorm:"column:value;type:text;not null"`
	Visibility int      `gorm:"column:visibility;not null;index:idx_subject_ns_vis_updated,priority:3"`
	Confidence *float64 `gorm:"column:confidence"`
	Source     *string  `gorm:"column:source;type:text"`
	CreatedAt  int64    `gorm:"column:created_at;not null"`
	UpdatedAt  int64    `gorm:"column:updated_at;not null;index:idx_subject_ns_vis_updated,priority:4"`
}

func (MemoryItem) TableName() string { return "memory_items" }
