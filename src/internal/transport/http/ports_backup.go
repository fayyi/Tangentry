package httptransport

import (
	"context"
	"tangentry/internal/backup"
)

type BackupService interface {
	Targets(context.Context) ([]backup.Target, error)
	Tasks(context.Context) ([]backup.Task, error)
	Snapshots(context.Context) ([]backup.Snapshot, error)
	SaveTarget(context.Context, backup.Target) error
	DeleteTarget(context.Context, string) error
	SaveTask(context.Context, backup.Task) error
	DeleteTask(context.Context, string) error
	Create(context.Context, string, string) (backup.Snapshot, error)
	Validate(context.Context, string, string, string) (backup.Validation, error)
	Restore(context.Context, string, string, string, string) (backup.RestoreResult, error)
	ConfirmRestore(context.Context, string) (backup.RestoreResult, error)
	CancelRestore(context.Context, string) error
}
