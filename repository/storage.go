package repository

import (
	"github.com/tigerowo/infinite-canvas/model"
)

// SaveStorageObject 保存存储对象记录。
func SaveStorageObject(object model.StorageObject) (model.StorageObject, error) {
	db, err := DB()
	if err != nil {
		return model.StorageObject{}, err
	}
	return object, db.Save(&object).Error
}

// GetStorageObject 根据 ID 获取存储对象。
func GetStorageObject(id string) (model.StorageObject, error) {
	db, err := DB()
	if err != nil {
		return model.StorageObject{}, err
	}
	var object model.StorageObject
	err = db.First(&object, "id = ?", id).Error
	return object, err
}

// DeleteStorageObjectRecord 删除存储对象记录（软删除）。
func DeleteStorageObjectRecord(id string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Delete(&model.StorageObject{}, "id = ?", id).Error
}

// GetStorageObjects returns the objects among ids, keyed by ID.
func GetStorageObjects(ids []string) (map[string]model.StorageObject, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	// ponytail: one IN query; chunk it if a response ever carries more file URLs than the driver's bind limit.
	objects := []model.StorageObject{}
	if err := db.Where("id IN ?", ids).Find(&objects).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]model.StorageObject, len(objects))
	for _, object := range objects {
		byID[object.ID] = object
	}
	return byID, nil
}

// MarkStorageObjectsPublic flags ids as public files.
func MarkStorageObjectsPublic(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.StorageObject{}).Where("id IN ?", ids).Update("public", true).Error
}
