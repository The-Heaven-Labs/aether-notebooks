package storage

import "io"

type Storage interface {
	Put(id string, r io.Reader, size int64, mimeType string) error
	Get(id string) (io.ReadCloser, error)
	Delete(id string) error
}
