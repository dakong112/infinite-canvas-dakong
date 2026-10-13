package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
)

// localStorageProviderID is stored on files written to LOCAL_STORAGE_DIR.
const localStorageProviderID = "local"

// localStorageProvider is the disk provider configured only through LOCAL_STORAGE_DIR,
// so it never appears in (or is saved back through) the admin settings form.
func localStorageProvider() (model.StorageProvider, bool) {
	dir := strings.TrimSpace(config.Cfg.LocalStorageDir)
	if dir == "" {
		return model.StorageProvider{}, false
	}
	return model.StorageProvider{ID: localStorageProviderID, Name: "本地磁盘", Type: model.StorageProviderTypeLocal, Endpoint: dir, Weight: 1, Enabled: true}, true
}

func localObjectPath(provider model.StorageProvider, objectKey string) (string, error) {
	clean, err := cleanStoragePath(objectKey)
	if err != nil {
		return "", err
	}
	return filepath.Join(provider.Endpoint, filepath.FromSlash(clean)), nil
}

func putLocalObject(provider model.StorageProvider, objectKey string, data []byte) error {
	target, err := localObjectPath(provider, objectKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func getLocalObjectStream(provider model.StorageProvider, objectKey string, rangeHeader string) (storageObjectStream, error) {
	target, err := localObjectPath(provider, objectKey)
	if err != nil {
		return storageObjectStream{}, err
	}
	file, err := os.Open(target)
	if err != nil {
		return storageObjectStream{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return storageObjectStream{}, err
	}
	size := info.Size()
	if byteRange, ok := parseStorageByteRange(rangeHeader, size); ok {
		if _, err := file.Seek(byteRange.offset, io.SeekStart); err != nil {
			_ = file.Close()
			return storageObjectStream{}, err
		}
		return storageObjectStream{
			Body: struct {
				io.Reader
				io.Closer
			}{io.LimitReader(file, byteRange.length), file},
			StatusCode:    206,
			ContentLength: byteRange.length,
			ContentRange:  fmt.Sprintf("bytes %d-%d/%d", byteRange.offset, byteRange.offset+byteRange.length-1, size),
			AcceptRanges:  true,
		}, nil
	}
	return storageObjectStream{Body: file, StatusCode: 200, ContentLength: size, AcceptRanges: true}, nil
}

// ponytail: empty date directories are left behind; prune them if disk inode use matters.
func deleteLocalObject(provider model.StorageProvider, objectKey string) error {
	target, err := localObjectPath(provider, objectKey)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
