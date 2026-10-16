package repository

import (
	"errors"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// generationClaimScan bounds how many due jobs one claim locks and inspects.
const generationClaimScan = 20

var finishedGenerationStatuses = []string{model.GenerationStatusSucceeded, model.GenerationStatusFailed, model.GenerationStatusCancelled}

// ActiveGenerationStatuses are the states of a job that has not finished yet.
var ActiveGenerationStatuses = []string{model.GenerationStatusQueued, model.GenerationStatusRunning, model.GenerationStatusPolling}

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

// CreateGenerationJobs appends jobs to an existing batch.
func CreateGenerationJobs(jobs []model.GenerationJob) error {
	if len(jobs) == 0 {
		return nil
	}
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Create(&jobs).Error
}

// ownedBy scopes rows to an owner (service.SpaceOwner): a team space id ("team:<id>") matches every
// member's rows of that space; anything else is a user id and matches that user's personal rows.
func ownedBy(query *gorm.DB, owner string) *gorm.DB {
	if strings.HasPrefix(owner, model.TeamSpacePrefix) {
		return query.Where("space_id = ?", owner)
	}
	return query.Where("user_id = ? AND space_id = ''", owner)
}

func GetUserGenerationBatch(owner string, id string) (model.GenerationBatch, bool, error) {
	db, err := DB()
	if err != nil {
		return model.GenerationBatch{}, false, err
	}
	var batch model.GenerationBatch
	err = ownedBy(db, owner).Where("id = ?", id).First(&batch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.GenerationBatch{}, false, nil
	}
	return batch, err == nil, err
}

// UpdateGenerationBatchContext writes new state only if nobody else wrote since version was read.
func UpdateGenerationBatchContext(userID string, id string, version int, contextJSON string) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := db.Model(&model.GenerationBatch{}).
		Where("user_id = ? AND id = ? AND version = ?", userID, id, version).
		Updates(map[string]any{"context_json": contextJSON, "version": version + 1})
	return update.Error == nil && update.RowsAffected == 1, update.Error
}

func ListUserGenerationBatches(owner string, source string, limit int) ([]model.GenerationBatch, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	batches := []model.GenerationBatch{}
	err = ownedBy(db, owner).Where("source = ?", source).Order("created_at DESC, id DESC").Limit(limit).Find(&batches).Error
	return batches, err
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
		Updates(waivePendingFee(values))
	return update.RowsAffected == 1, update.Error
}

// waivePendingFee drops a still-pending template fee when the job ends without a result.
func waivePendingFee(values map[string]any) map[string]any {
	if values["status"] == model.GenerationStatusFailed || values["status"] == model.GenerationStatusCancelled {
		values["fee_status"] = gorm.Expr("CASE WHEN fee_status = ? THEN ? ELSE fee_status END", model.TemplateFeePending, model.TemplateFeeWaived)
	}
	return values
}

// RecoverExpiredGenerationJobs requeues running jobs whose worker vanished, failing those out of attempts.
func RecoverExpiredGenerationJobs(now time.Time) error {
	db, err := DB()
	if err != nil {
		return err
	}
	if err := db.Model(&model.GenerationJob{}).
		Where("status = ? AND lease_expires_at <= ? AND attempts >= max_attempts", model.GenerationStatusRunning, now).
		Updates(waivePendingFee(map[string]any{"status": model.GenerationStatusFailed, "error": "任务执行中断且已达到最大重试次数", "finished_at": now, "lease_token": "", "lease_expires_at": nil})).Error; err != nil {
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

func GetUserGenerationJob(owner string, id string) (model.GenerationJob, bool, error) {
	db, err := DB()
	if err != nil {
		return model.GenerationJob{}, false, err
	}
	var job model.GenerationJob
	err = ownedBy(db, owner).Where("id = ? AND deleted_at IS NULL", id).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.GenerationJob{}, false, nil
	}
	return job, err == nil, err
}

func ListUserGenerationJobs(owner string, ids []string) ([]model.GenerationJob, error) {
	jobs := []model.GenerationJob{}
	if len(ids) == 0 {
		return jobs, nil
	}
	db, err := DB()
	if err != nil {
		return nil, err
	}
	err = ownedBy(db, owner).Where("id IN ? AND deleted_at IS NULL", ids).Find(&jobs).Error
	return jobs, err
}

func ListUserBatchGenerationJobs(owner string, batchID string) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	jobs := []model.GenerationJob{}
	err = ownedBy(db, owner).Where("batch_id = ? AND deleted_at IS NULL", batchID).Order("created_at ASC, id ASC").Find(&jobs).Error
	return jobs, err
}

func CancelUserGenerationJob(owner string, id string, now time.Time) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := ownedBy(db.Model(&model.GenerationJob{}), owner).
		Where("id = ? AND status IN ? AND deleted_at IS NULL", id, ActiveGenerationStatuses).
		Updates(waivePendingFee(map[string]any{"status": model.GenerationStatusCancelled, "error": "任务已取消", "finished_at": now, "lease_token": "", "lease_expires_at": nil}))
	return update.RowsAffected == 1, update.Error
}

func RetryUserGenerationJob(owner string, id string, now time.Time) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := ownedBy(db.Model(&model.GenerationJob{}), owner).
		Where("id = ? AND status IN ? AND deleted_at IS NULL AND NOT (kind = ? AND source = ?)", id, []string{model.GenerationStatusFailed, model.GenerationStatusCancelled}, model.GenerationKindVision, model.GenerationSourceEcommerceSet).
		// A template fee waived when the job failed is owed again if the retry succeeds.
		Updates(map[string]any{"status": model.GenerationStatusQueued, "attempts": 0, "error": "", "next_run_at": now, "finished_at": nil, "lease_token": "", "lease_expires_at": nil,
			"fee_status": gorm.Expr("CASE WHEN fee_quota > 0 THEN ? ELSE fee_status END", model.TemplateFeePending)})
	return update.RowsAffected == 1, update.Error
}

// ListUserWorks pages a user's image jobs newest first; cursorAt/cursorID is the last row of the previous page.
func ListUserWorks(owner string, source string, templateID string, statuses []string, cursorAt time.Time, cursorID string, limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	query := worksQuery(db.Model(&model.GenerationJob{}), owner, source, templateID)
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
func CountUserWorks(owner string, source string, templateID string) (map[string]int64, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	query := worksQuery(db.Model(&model.GenerationJob{}), owner, source, templateID)
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

// worksQuery scopes the works page to the user's results: images (except product video keyframes) and
// composed videos, optionally of one source or template.
func worksQuery(query *gorm.DB, owner string, source string, templateID string) *gorm.DB {
	query = ownedBy(query, owner).Where("deleted_at IS NULL").
		Where("((kind = ? AND source <> ?) OR kind = ?)", model.GenerationKindImage, model.GenerationSourceProductVideo, model.GenerationKindCompose)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if templateID != "" {
		query = query.Where("template_id = ?", templateID)
	}
	return query
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
		Where("user_id = ? AND status IN ?", userID, ActiveGenerationStatuses).
		Count(&total).Error
	return total, err
}

// ClaimPollingGenerationJobs takes due polling jobs by pushing their next poll to leaseUntil, so a
// worker that dies mid-poll only delays the job; the guarded update gives each job to one worker.
func ClaimPollingGenerationJobs(now time.Time, leaseUntil time.Time, limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var due []model.GenerationJob
	if err := db.Where("status = ? AND next_run_at <= ?", model.GenerationStatusPolling, now).Order("next_run_at ASC").Limit(limit).Find(&due).Error; err != nil {
		return nil, err
	}
	claimed := due[:0]
	for _, job := range due {
		update := db.Model(&model.GenerationJob{}).Where("id = ? AND status = ? AND next_run_at <= ?", job.ID, model.GenerationStatusPolling, now).Update("next_run_at", leaseUntil)
		if update.Error != nil {
			return claimed, update.Error
		}
		if update.RowsAffected == 1 {
			claimed = append(claimed, job)
		}
	}
	return claimed, nil
}

// UpdatePollingGenerationJob records a poll outcome if the job is still polling (not cancelled meanwhile).
func UpdatePollingGenerationJob(id string, values map[string]any) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	update := db.Model(&model.GenerationJob{}).Where("id = ? AND status = ?", id, model.GenerationStatusPolling).Updates(waivePendingFee(values))
	return update.RowsAffected == 1, update.Error
}
