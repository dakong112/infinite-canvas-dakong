package service

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
)

// localStorageProviderID is stored on files written to LOCAL_STORAGE_DIR.
const localStorageProviderID = "local"

var localStorageProbe sync.Map // directory -> struct{}, probed writable once per process

// probeLocalStorage creates LOCAL_STORAGE_DIR and checks it is writable, logging a clear line on the first failure.
func probeLocalStorage(dir string) {
	if _, done := localStorageProbe.LoadOrStore(dir, struct{}{}); done {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("LOCAL_STORAGE_DIR %q is not usable: %v", dir, err)
		return
	}
	probe := filepath.Join(dir, ".write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		log.Printf("LOCAL_STORAGE_DIR %q is not writable: %v", dir, err)
		return
	}
	_ = os.Remove(probe)
}

// localStorageProvider is the disk provider configured only through LOCAL_STORAGE_DIR,
// so it never appears in (or is saved back through) the admin settings form.
func localStorageProvider() (model.StorageProvider, bool) {
	dir := strings.TrimSpace(config.Cfg.LocalStorageDir)
	if dir == "" {
		return model.StorageProvider{}, false
	}
	probeLocalStorage(dir)
	return model.StorageProvider{ID: localStorageProviderID, Name: "本地磁盘", Type: model.StorageProviderTypeLocal, Endpoint: dir, Weight: 1, Enabled: true}, true
}

// localObjectKey rejects keys that could leave the storage directory on any OS; os.Root enforces it again at open time.
func localObjectKey(objectKey string) (string, error) {
	if strings.HasPrefix(objectKey, "/") || strings.ContainsAny(objectKey, "\\:\x00") {
		return "", errors.New("存储路径格式不正确")
	}
	return cleanStoragePath(objectKey)
}

// openLocalRoot opens the provider directory as a root, so no key can resolve outside it, even through symlinks.
func openLocalRoot(provider model.StorageProvider, create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(provider.Endpoint, 0o755); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(provider.Endpoint)
}

func putLocalObject(provider model.StorageProvider, objectKey string, data []byte) error {
	key, err := localObjectKey(objectKey)
	if err != nil {
		return err
	}
	root, err := openLocalRoot(provider, true)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(path.Dir(key), 0o755); err != nil {
		return err
	}
	return root.WriteFile(key, data, 0o644)
}

func getLocalObjectStream(provider model.StorageProvider, objectKey string, rangeHeader string) (storageObjectStream, error) {
	key, err := localObjectKey(objectKey)
	if err != nil {
		return storageObjectStream{}, err
	}
	root, err := openLocalRoot(provider, false)
	if err != nil {
		return storageObjectStream{}, err
	}
	defer root.Close()
	file, err := root.Open(key)
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
	key, err := localObjectKey(objectKey)
	if err != nil {
		return err
	}
	root, err := openLocalRoot(provider, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer root.Close()
	if err := root.Remove(key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
