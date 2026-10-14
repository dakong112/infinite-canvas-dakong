package repository

import (
	"errors"

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

// ListPendingTemplateFeeJobs returns succeeded jobs whose template fee is not settled yet.
func ListPendingTemplateFeeJobs(limit int) ([]model.GenerationJob, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	jobs := []model.GenerationJob{}
	err = db.Where("fee_status = ? AND status = ?", model.TemplateFeePending, model.GenerationStatusSucceeded).
		Order("finished_at ASC").Limit(limit).Find(&jobs).Error
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
