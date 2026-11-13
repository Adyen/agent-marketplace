package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ConfigArchiveTarget names the backups directory that holds user configuration archives.
const ConfigArchiveTarget = "agents-config"

type ConfigFile struct {
	Name    string
	Data    []byte
	Mode    fs.FileMode
	ModTime time.Time
}

func CreateConfigArchive(files []ConfigFile) (string, error) {
	if len(files) == 0 {
		return "", nil
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	timestamp := time.Now().Format("2006-01-02-150405")
	archivePath, err := nextArchivePath(filepath.Join(dir, ConfigArchiveTarget), timestamp)
	if err != nil {
		return "", err
	}
	if err := withArchiveOutput(archivePath, func(file *os.File) error {
		gz := gzip.NewWriter(file)
		tw := tar.NewWriter(gz)
		var writeErr error
		for _, configFile := range files {
			header := &tar.Header{
				Name:     configFile.Name,
				Mode:     int64(configFile.Mode.Perm()),
				Size:     int64(len(configFile.Data)),
				ModTime:  configFile.ModTime,
				Typeflag: tar.TypeReg,
			}
			if err := tw.WriteHeader(header); err != nil {
				writeErr = err
				break
			}
			if _, err := tw.Write(configFile.Data); err != nil {
				writeErr = err
				break
			}
		}
		return errors.Join(writeErr, tw.Close(), gz.Close())
	}); err != nil {
		return "", err
	}
	return archivePath, nil
}
