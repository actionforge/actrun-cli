package nodes

import (
	"io"

	"github.com/actionforge/actrun-cli/core"
)

type StorageList struct {
	Objects []string
	Dirs    []string
}

type StorageListProvider interface {
	ListObjects(dir string) (StorageList, error)
}

type StorageCloneProvider interface {
	CanClone(src core.StorageProvider) bool
	CloneObject(dstName string, src core.StorageProvider, srcName string) error
}

type StorageDeleteProvider interface {
	DeleteFile(name string) error
}

type StorageUploadProvider interface {
	UploadObject(name string, data io.Reader) error
}

type StorageDownloadProvider interface {
	DownloadObject(name string) (io.Reader, error)
}
