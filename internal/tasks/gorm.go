package tasks

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PersistentTask is the database representation of a queued task. Payload is
// intentionally opaque so applications can deserialize it into a core.Task.
type PersistentTask struct {
	ID          string `gorm:"primaryKey"`
	Kind        string `gorm:"index"`
	TenantID    string `gorm:"index"`
	Payload     []byte `gorm:"type:jsonb"`
	Status      Status `gorm:"index"`
	Attempts    int
	LockedBy    string `gorm:"index"`
	LockedUntil *time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type GORMStore struct{ DB *gorm.DB }

func NewGORMStore(db *gorm.DB) *GORMStore { return &GORMStore{DB: db} }
func (s *GORMStore) Migrate(ctx context.Context) error {
	return s.DB.WithContext(ctx).AutoMigrate(&PersistentTask{})
}
func (s *GORMStore) Enqueue(ctx context.Context, task PersistentTask) error {
	if task.Status == "" {
		task.Status = Queued
	}
	return s.DB.WithContext(ctx).Create(&task).Error
}
func (s *GORMStore) Get(ctx context.Context, id string) (PersistentTask, error) {
	var t PersistentTask
	err := s.DB.WithContext(ctx).First(&t, "id = ?", id).Error
	return t, err
}
func (s *GORMStore) Claim(ctx context.Context, worker string, lease time.Duration) (PersistentTask, error) {
	var task PersistentTask
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("status = ? AND (locked_until IS NULL OR locked_until < ?)", Queued, time.Now().UTC()).Order("created_at").First(&task)
		if query.Error != nil {
			return query.Error
		}
		now := time.Now().UTC()
		expires := now.Add(lease)
		task.Status = Running
		task.Attempts++
		task.LockedBy = worker
		task.LockedUntil = &expires
		return tx.Save(&task).Error
	})
	return task, err
}
func (s *GORMStore) Complete(ctx context.Context, id string, errValue error) error {
	updates := map[string]any{"status": Completed, "locked_until": nil, "locked_by": ""}
	if errValue != nil {
		updates["status"] = Failed
		updates["last_error"] = errValue.Error()
	}
	return s.DB.WithContext(ctx).Model(&PersistentTask{}).Where("id = ?", id).Updates(updates).Error
}
