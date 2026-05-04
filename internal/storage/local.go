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

func NewLocalStorage(dir string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("storage: mkdir %s: %w", dir, err)
	}
	return &LocalStorage{dir: dir}, nil
}

func (l *LocalStorage) Put(id string, r io.Reader, _ int64, _ string) error {
	path := filepath.Join(l.dir, id)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (l *LocalStorage) Get(id string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(l.dir, id))
}

func (l *LocalStorage) Delete(id string) error {
	err := os.Remove(filepath.Join(l.dir, id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
