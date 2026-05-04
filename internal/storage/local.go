package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type LocalStorage struct {
	dir string
}

func NewLocalStorage(dir string) (Storage, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("storage: mkdir %s: %w", dir, err)
	}
	return &LocalStorage{dir: dir}, nil
}

func (l *LocalStorage) Put(id string, r io.Reader, _ int64, _ string) error {
	tmp, err := os.CreateTemp(l.dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("storage: create temp: %w", err)
	}
	tmpName := tmp.Name()

	_, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmpName)
		if copyErr != nil {
			return fmt.Errorf("storage: write %s: %w", id, copyErr)
		}
		return fmt.Errorf("storage: close %s: %w", id, closeErr)
	}

	dest := filepath.Join(l.dir, id)
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("storage: commit %s: %w", id, err)
	}
	return nil
}

func (l *LocalStorage) Get(id string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(l.dir, id))
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", id, err)
	}
	return f, nil
}

func (l *LocalStorage) Delete(id string) error {
	err := os.Remove(filepath.Join(l.dir, id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete %s: %w", id, err)
	}
	return nil
}
