package repository

import (
	"errors"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
)

// ListImageTemplates returns templates by sort; empty status or category means any.
func ListImageTemplates(status string, category string) ([]model.ImageTemplate, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	query := db.Model(&model.ImageTemplate{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	items := []model.ImageTemplate{}
	err = query.Order("sort ASC, created_at ASC").Find(&items).Error
	return items, err
}

func GetImageTemplate(id string) (model.ImageTemplate, bool, error) {
	return findImageTemplate("id = ?", id)
}

func GetImageTemplateBySlug(slug string) (model.ImageTemplate, bool, error) {
	return findImageTemplate("slug = ?", slug)
}

func findImageTemplate(where string, value string) (model.ImageTemplate, bool, error) {
	db, err := DB()
	if err != nil {
		return model.ImageTemplate{}, false, err
	}
	var item model.ImageTemplate
	err = db.Where(where, value).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ImageTemplate{}, false, nil
	}
	return item, err == nil, err
}

func SaveImageTemplate(item model.ImageTemplate) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Save(&item).Error
}

func DeleteImageTemplate(id string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Delete(&model.ImageTemplate{}, "id = ?", id).Error
}

// SeedImageTemplates inserts items only while the table is empty.
func SeedImageTemplates(items []model.ImageTemplate) error {
	db, err := DB()
	if err != nil {
		return err
	}
	var total int64
	if err := db.Model(&model.ImageTemplate{}).Count(&total).Error; err != nil || total > 0 {
		return err
	}
	return db.Create(&items).Error
}

// ListPendingTemplateFeeJobs returns succeeded jobs whose template fee is not settled yet and is due by now.
func ListPendingTemplateFeeJobs(now time.Time, limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	jobs := []model.GenerationJob{}
	err = db.Where("fee_status = ? AND status = ? AND (fee_next_at IS NULL OR fee_next_at <= ?)", model.TemplateFeePending, model.GenerationStatusSucceeded, now).
		Order("finished_at ASC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// ListTemplateFeeJobs returns the latest jobs with the given template fee status.
func ListTemplateFeeJobs(status string, limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	jobs := []model.GenerationJob{}
	err = db.Where("fee_status = ?", status).Order("finished_at DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// UpdatePendingTemplateFee writes a settlement outcome only while the fee is still pending.
func UpdatePendingTemplateFee(id string, values map[string]any) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.GenerationJob{}).Where("id = ? AND fee_status = ?", id, model.TemplateFeePending).Updates(values).Error
}

// ListTemplateExampleJobs returns the latest admin example runs, of one template when templateID is set.
func ListTemplateExampleJobs(templateID string, limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	query := db.Where("source = ? AND deleted_at IS NULL", model.GenerationSourceTemplateExample)
	if templateID != "" {
		query = query.Where("template_id = ?", templateID)
	}
	jobs := []model.GenerationJob{}
	err = query.Order("created_at DESC, id DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// KeepGenerationOutputs moves the expiry of jobs whose output is one of fileIDs to until.
func KeepGenerationOutputs(fileIDs []string, until time.Time) error {
	if len(fileIDs) == 0 {
		return nil
	}
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.GenerationJob{}).Where("output_file_id IN ? AND deleted_at IS NULL", fileIDs).Update("expires_at", until).Error
}
