package repository

import (
	"errors"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListAcademyCourses returns courses by sort; empty status or category means any.
func ListAcademyCourses(status string, category string) ([]model.AcademyCourse, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	query := db.Model(&model.AcademyCourse{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	items := []model.AcademyCourse{}
	err = query.Order("sort ASC, id DESC").Find(&items).Error
	return items, err
}

func GetAcademyCourse(id uint) (model.AcademyCourse, bool, error) {
	return findAcademyCourse("id = ?", id)
}

func GetAcademyCourseBySlug(slug string) (model.AcademyCourse, bool, error) {
	return findAcademyCourse("slug = ?", slug)
}

func findAcademyCourse(where string, value any) (model.AcademyCourse, bool, error) {
	db, err := DB()
	if err != nil {
		return model.AcademyCourse{}, false, err
	}
	var item model.AcademyCourse
	err = db.Where(where, value).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AcademyCourse{}, false, nil
	}
	return item, err == nil, err
}

func CreateAcademyCourse(item *model.AcademyCourse) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Create(item).Error
}

// UpdateAcademyCourse writes every field except the seat counter and the creation time.
func UpdateAcademyCourse(item model.AcademyCourse) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.AcademyCourse{}).Where("id = ?", item.ID).Select("*").Omit("id", "enrolled_count", "created_at").Updates(&item).Error
}

func DeleteAcademyCourse(id uint) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Delete(&model.AcademyCourse{}, "id = ?", id).Error
}

// SeedAcademyCourses inserts items only while the table is empty.
func SeedAcademyCourses(items []model.AcademyCourse) error {
	db, err := DB()
	if err != nil {
		return err
	}
	var total int64
	if err := db.Model(&model.AcademyCourse{}).Count(&total).Error; err != nil || total > 0 {
		return err
	}
	return db.Create(&items).Error
}

// ReserveAcademySeat takes one seat in a single UPDATE, so concurrent enrolls never pass the capacity.
func ReserveAcademySeat(id uint) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	result := db.Model(&model.AcademyCourse{}).Where("id = ? AND (capacity = 0 OR enrolled_count < capacity)", id).
		Update("enrolled_count", gorm.Expr("enrolled_count + 1"))
	return result.RowsAffected == 1, result.Error
}

func ReleaseAcademySeat(id uint) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.AcademyCourse{}).Where("id = ? AND enrolled_count > 0", id).Update("enrolled_count", gorm.Expr("enrolled_count - 1")).Error
}

func GetAcademyEnrollment(courseID uint, userID string) (model.AcademyEnrollment, bool, error) {
	db, err := DB()
	if err != nil {
		return model.AcademyEnrollment{}, false, err
	}
	var item model.AcademyEnrollment
	err = db.Where("course_id = ? AND user_id = ?", courseID, userID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AcademyEnrollment{}, false, nil
	}
	return item, err == nil, err
}

// CreateAcademyEnrollment inserts item; false means the user already holds an enrollment in the course.
func CreateAcademyEnrollment(item *model.AcademyEnrollment) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(item)
	return result.RowsAffected == 1, result.Error
}

func CountAcademyEnrollments(courseID uint) (int64, error) {
	db, err := DB()
	if err != nil {
		return 0, err
	}
	var total int64
	err = db.Model(&model.AcademyEnrollment{}).Where("course_id = ?", courseID).Count(&total).Error
	return total, err
}

func ListUserAcademyEnrollments(userID string) ([]model.AcademyEnrollment, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	items := []model.AcademyEnrollment{}
	err = db.Where("user_id = ?", userID).Order("id DESC").Find(&items).Error
	return items, err
}

func ListAcademyCourseEnrollments(courseID uint) ([]model.AcademyEnrollmentRow, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	items := []model.AcademyEnrollmentRow{}
	err = db.Model(&model.AcademyEnrollment{}).Select("academy_enrollments.*, users.username").
		Joins("LEFT JOIN users ON users.id = academy_enrollments.user_id").
		Where("academy_enrollments.course_id = ?", courseID).Order("academy_enrollments.id DESC").Scan(&items).Error
	return items, err
}
