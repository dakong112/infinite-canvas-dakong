package repository

import (
	"errors"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// generationClaimScan bounds how many due jobs one claim locks and inspects.
const generationClaimScan = 20

var finishedGenerationStatuses = []string{model.GenerationStatusSucceeded, model.GenerationStatusFailed, model.GenerationStatusCancelled}

// CreateGenerationBatch stores a batch and its jobs in one transaction.
func CreateGenerationBatch(batch model.GenerationBatch, jobs []model.GenerationJob) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		return tx.Create(&jobs).Error
	})
}

// GenerationClaimExclusion lists users that must not get another job right now, so a user at their
// concurrency cap with a long queue cannot fill the scan window and starve everyone else.
type GenerationClaimExclusion struct {
	// Users are at the member cap: none of their jobs may start.
	Users []string
	// NonMemberUsers are at the non-member cap: only their non-member jobs are blocked.
	NonMemberUsers []string
}

// ClaimGenerationJob leases the oldest due queued job that allow accepts.
// Row locks queue concurrent claimers on MySQL/PostgreSQL (SQLite drops the clause and serializes
// writers itself); the status-guarded update is what guarantees a job goes to exactly one worker.
func ClaimGenerationJob(now time.Time, leaseToken string, leaseUntil time.Time, exclude GenerationClaimExclusion, allow func(model.GenerationJob) bool) (*model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var claimed *model.GenerationJob
	err = db.Transaction(func(tx *gorm.DB) error {
		var candidates []model.GenerationJob
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status = ? AND next_run_at <= ?", model.GenerationStatusQueued, now)
		if len(exclude.Users) > 0 {
			query = query.Where("user_id NOT IN ?", exclude.Users)
		}
		if len(exclude.NonMemberUsers) > 0 {
			query = query.Where("NOT (member = ? AND user_id IN ?)", false, exclude.NonMemberUsers)
		}
		if err := query.
			Order("next_run_at ASC, created_at ASC").
			Limit(generationClaimScan).
			Find(&candidates).Error; err != nil {
			return err
		}
		for _, job := range candidates {
			if !allow(job) {
				continue
			}
			update := tx.Model(&model.GenerationJob{}).
				Where("id = ? AND status = ?", job.ID, model.GenerationStatusQueued).
				Updates(map[string]any{
					"status":           model.GenerationStatusRunning,
					"attempts":         gorm.Expr("attempts + 1"),
					"lease_token":      leaseToken,
					"lease_expires_at": leaseUntil,
				})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected == 1 {
				job.Status = model.GenerationStatusRunning
				job.Attempts++
				job.LeaseToken = leaseToken
				job.LeaseExpiresAt = &leaseUntil
				claimed = &job
				return nil
			}
		}
		return nil
	})
	return claimed, err
}

// RenewGenerationLease extends a running job's lease; false means the job was cancelled or taken over.
func RenewGenerationLease(id string, leaseToken string, leaseUntil time.Time) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := db.Model(&model.GenerationJob{}).
		Where("id = ? AND lease_token = ? AND status = ?", id, leaseToken, model.GenerationStatusRunning).
		Update("lease_expires_at", leaseUntil)
	return update.RowsAffected == 1, update.Error
}

// FinishGenerationJob writes an attempt's outcome if the caller still holds the lease.
func FinishGenerationJob(id string, leaseToken string, values map[string]any) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := db.Model(&model.GenerationJob{}).
		Where("id = ? AND lease_token = ? AND status = ?", id, leaseToken, model.GenerationStatusRunning).
		Updates(values)
	return update.RowsAffected == 1, update.Error
}

// RecoverExpiredGenerationJobs requeues running jobs whose worker vanished, failing those out of attempts.
func RecoverExpiredGenerationJobs(now time.Time) error {
	db, err := DB()
	if err != nil {
		return err
	}
	if err := db.Model(&model.GenerationJob{}).
		Where("status = ? AND lease_expires_at <= ? AND attempts >= max_attempts", model.GenerationStatusRunning, now).
		Updates(map[string]any{"status": model.GenerationStatusFailed, "error": "任务执行中断且已达到最大重试次数", "finished_at": now, "lease_token": "", "lease_expires_at": nil}).Error; err != nil {
		return err
	}
	return db.Model(&model.GenerationJob{}).
		Where("status = ? AND lease_expires_at <= ?", model.GenerationStatusRunning, now).
		Updates(map[string]any{"status": model.GenerationStatusQueued, "next_run_at": now, "lease_token": "", "lease_expires_at": nil}).Error
}

// RunningGenerationJobCounts returns running jobs per user across all workers.
func RunningGenerationJobCounts() (map[string]int, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var rows []struct {
		UserID string
		Total  int
	}
	if err := db.Model(&model.GenerationJob{}).Select("user_id, count(*) as total").
		Where("status = ?", model.GenerationStatusRunning).Group("user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.UserID] = row.Total
	}
	return counts, nil
}

func GetUserGenerationJob(userID string, id string) (model.GenerationJob, bool, error) {
	db, err := DB()
	if err != nil {
		return model.GenerationJob{}, false, err
	}
	var job model.GenerationJob
	err = db.Where("user_id = ? AND id = ? AND deleted_at IS NULL", userID, id).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.GenerationJob{}, false, nil
	}
	return job, err == nil, err
}

func ListUserGenerationJobs(userID string, ids []string) ([]model.GenerationJob, error) {
	jobs := []model.GenerationJob{}
	if len(ids) == 0 {
		return jobs, nil
	}
	db, err := DB()
	if err != nil {
		return nil, err
	}
	err = db.Where("user_id = ? AND id IN ? AND deleted_at IS NULL", userID, ids).Find(&jobs).Error
	return jobs, err
}

func ListUserBatchGenerationJobs(userID string, batchID string) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	jobs := []model.GenerationJob{}
	err = db.Where("user_id = ? AND batch_id = ? AND deleted_at IS NULL", userID, batchID).Order("created_at ASC, id ASC").Find(&jobs).Error
	return jobs, err
}

func CancelUserGenerationJob(userID string, id string, now time.Time) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := db.Model(&model.GenerationJob{}).
		Where("user_id = ? AND id = ? AND status IN ? AND deleted_at IS NULL", userID, id, []string{model.GenerationStatusQueued, model.GenerationStatusRunning}).
		Updates(map[string]any{"status": model.GenerationStatusCancelled, "error": "任务已取消", "finished_at": now, "lease_token": "", "lease_expires_at": nil})
	return update.RowsAffected == 1, update.Error
}

func RetryUserGenerationJob(userID string, id string, now time.Time) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := db.Model(&model.GenerationJob{}).
		Where("user_id = ? AND id = ? AND status IN ? AND deleted_at IS NULL", userID, id, []string{model.GenerationStatusFailed, model.GenerationStatusCancelled}).
		Updates(map[string]any{"status": model.GenerationStatusQueued, "attempts": 0, "error": "", "next_run_at": now, "finished_at": nil, "lease_token": "", "lease_expires_at": nil})
	return update.RowsAffected == 1, update.Error
}

// ListUserWorks pages a user's image jobs newest first; cursorAt/cursorID is the last row of the previous page.
func ListUserWorks(userID string, source string, statuses []string, cursorAt time.Time, cursorID string, limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	query := db.Where("user_id = ? AND kind = ? AND deleted_at IS NULL", userID, model.GenerationKindImage)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	if !cursorAt.IsZero() {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", cursorAt, cursorAt, cursorID)
	}
	jobs := []model.GenerationJob{}
	err = query.Order("created_at DESC, id DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// CountUserWorks returns image job counts per status for the works page counters.
func CountUserWorks(userID string, source string) (map[string]int64, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	query := db.Model(&model.GenerationJob{}).Where("user_id = ? AND kind = ? AND deleted_at IS NULL", userID, model.GenerationKindImage)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	var rows []struct {
		Status string
		Total  int64
	}
	if err := query.Select("status, count(*) as total").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Status] = row.Total
	}
	return counts, nil
}

// ListExpiredGenerationJobs returns finished, not yet deleted jobs past their retention.
func ListExpiredGenerationJobs(now time.Time, limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	jobs := []model.GenerationJob{}
	err = db.Where("deleted_at IS NULL AND expires_at <= ? AND status IN ?", now, finishedGenerationStatuses).
		Order("expires_at ASC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// ponytail: soft-deleted rows stay for audit; hard-delete them in bulk once the table grows large.
func MarkGenerationJobDeleted(id string, now time.Time) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.GenerationJob{}).Where("id = ?", id).Update("deleted_at", now).Error
}

// CountUserActiveGenerationJobs counts a user's queued and running jobs.
func CountUserActiveGenerationJobs(userID string) (int64, error) {
	db, err := DB()
	if err != nil {
		return 0, err
	}
	var total int64
	err = db.Model(&model.GenerationJob{}).
		Where("user_id = ? AND status IN ?", userID, []string{model.GenerationStatusQueued, model.GenerationStatusRunning}).
		Count(&total).Error
	return total, err
}
