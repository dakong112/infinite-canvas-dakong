package repository

import (
	"encoding/json"

	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetSettings 返回 public 和 private 两行配置。
func GetSettings() (model.Settings, error) {
	db, err := DB()
	if err != nil {
		return model.Settings{}, err
	}
	return readSettings(db)
}

func readSettings(db *gorm.DB) (model.Settings, error) {
	var items []model.Setting
	if err := db.Find(&items).Error; err != nil {
		return model.Settings{}, err
	}
	result := model.Settings{}
	for _, item := range items {
		if item.Key == model.SettingKeyPrivate {
			_ = json.Unmarshal(item.Value, &result.Private)
		} else if item.Key == model.SettingKeyPublic {
			_ = json.Unmarshal(item.Value, &result.Public)
		}
	}
	return result, nil
}

// UpdateSettings applies change to the stored settings in a transaction that locks their rows (SQLite has one
// connection), so concurrent read-modify-writes such as SaveSettings and SaveTemplateSamples cannot lose each other's fields.
func UpdateSettings(now string, change func(*model.Settings) error) (model.Settings, error) {
	db, err := DB()
	if err != nil {
		return model.Settings{}, err
	}
	var result model.Settings
	err = db.Transaction(func(tx *gorm.DB) error {
		if result, err = readSettings(tx.Clauses(clause.Locking{Strength: "UPDATE"})); err != nil {
			return err
		}
		if err := change(&result); err != nil {
			return err
		}
		return writeSettings(tx, result, now)
	})
	return result, err
}

// SaveSettings 保存 public 和 private 两行配置。
func SaveSettings(settings model.Settings, now string) (model.Settings, error) {
	db, err := DB()
	if err != nil {
		return settings, err
	}
	return settings, writeSettings(db, settings, now)
}

func writeSettings(db *gorm.DB, settings model.Settings, now string) error {
	publicValue, _ := json.Marshal(settings.Public)
	privateValue, _ := json.Marshal(settings.Private)
	items := []model.Setting{
		{Key: model.SettingKeyPublic, Value: publicValue, CreatedAt: now, UpdatedAt: now},
		{Key: model.SettingKeyPrivate, Value: privateValue, CreatedAt: now, UpdatedAt: now},
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&items).Error
}
