package repository

import (
	"errors"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Canvas projects take an owner (service.SpaceOwner): the user id for personal projects, or the team
// space id, where every member reads and writes the same projects.

func ListUserCanvasProjects(owner string) ([]model.CanvasProject, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}

	var projects []model.CanvasProject
	err = ownedBy(db, strings.TrimSpace(owner)).Where("deleted_at = ''").Order("updated_at DESC").Find(&projects).Error
	return projects, err
}

// SaveUserCanvasProject creates project (UserID and SpaceID set by the caller) or updates the owner's
// project of the same id when project is newer.
func SaveUserCanvasProject(
	owner string,
	project model.CanvasProject,
) (model.CanvasProject, error) {
	db, err := DB()
	if err != nil {
		return project, err
	}

	project.UserID = strings.TrimSpace(project.UserID)
	project.ID = strings.TrimSpace(project.ID)

	var current model.CanvasProject
	err = ownedBy(db, owner).First(&current, "id = ?", project.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return project, db.Create(&project).Error
	}
	if err != nil {
		return project, err
	}
	if current.DeletedAt != "" || current.UpdatedAt > project.UpdatedAt {
		return current, nil
	}

	result := db.Model(&model.CanvasProject{}).
		Where(
			"user_id = ? AND id = ? AND deleted_at = '' AND updated_at <= ?",
			current.UserID,
			current.ID,
			project.UpdatedAt,
		).
		Updates(map[string]any{
			"project_data": project.ProjectData,
			"updated_at":   project.UpdatedAt,
		})
	if result.Error != nil {
		return project, result.Error
	}
	if result.RowsAffected == 0 {
		if err := db.First(
			&current,
			"user_id = ? AND id = ?",
			current.UserID,
			current.ID,
		).Error; err != nil {
			return project, err
		}
		return current, nil
	}
	return project, nil
}

func SaveUserCanvasProjects(
	owner string,
	projects []model.CanvasProject,
) ([]model.CanvasProject, error) {
	for _, project := range projects {
		if _, err := SaveUserCanvasProject(owner, project); err != nil {
			return nil, err
		}
	}
	return ListUserCanvasProjects(owner)
}

// SoftDeleteUserCanvasProjects marks the owner's projects deleted and leaves a tombstone (in the
// user's name and space) for ids it has not seen yet, so a later sync cannot bring them back.
func SoftDeleteUserCanvasProjects(
	owner string,
	userID string,
	spaceID string,
	ids []string,
	deletedAt string,
) error {
	ids = uniqueTrimmedValues(ids...)
	if len(ids) == 0 {
		return nil
	}

	db, err := DB()
	if err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := ownedBy(tx.Model(&model.CanvasProject{}), owner).Where("id IN ?", ids).Updates(map[string]any{
			"project_data": "",
			"updated_at":   deletedAt,
			"deleted_at":   deletedAt,
		}).Error; err != nil {
			return err
		}
		records := make([]model.CanvasProject, 0, len(ids))
		for _, id := range ids {
			records = append(records, model.CanvasProject{
				UserID:    strings.TrimSpace(userID),
				SpaceID:   spaceID,
				ID:        id,
				CreatedAt: deletedAt,
				UpdatedAt: deletedAt,
				DeletedAt: deletedAt,
			})
		}
		// Rows that exist (in any space) keep their state: another space's project is never touched.
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&records).Error
	})
}

func CleanupDeletedCanvasProjects(before string) error {
	db, err := DB()
	if err != nil {
		return err
	}

	return db.Where(
		"deleted_at <> '' AND deleted_at < ?",
		before,
	).Delete(&model.CanvasProject{}).Error
}
