package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

const MaxEffectiveConfigSize int64 = 4 << 20

var ErrConfigTooLarge = errors.New("config file exceeds maximum size")
var ErrConfigNotRegular = errors.New("config path is not a regular file")

func boundedConfigData(data []byte) ([]byte, error) {
	if int64(len(data)) > MaxEffectiveConfigSize {
		return nil, fmt.Errorf("%w: maximum %d bytes", ErrConfigTooLarge, MaxEffectiveConfigSize)
	}
	return append([]byte(nil), data...), nil
}

func readConfigFile(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return readOpenedConfigFile(file)
}

func readOpenedConfigFile(file *os.File) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat config %q: %w", file.Name(), err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read config %q: %w", file.Name(), ErrConfigNotRegular)
	}
	if info.Size() > MaxEffectiveConfigSize {
		return nil, fmt.Errorf("%w: maximum %d bytes", ErrConfigTooLarge, MaxEffectiveConfigSize)
	}
	if err := syscall.SetNonblock(int(file.Fd()), false); err != nil {
		return nil, fmt.Errorf("restore blocking config reads %q: %w", file.Name(), err)
	}
	limited := io.LimitReader(file, MaxEffectiveConfigSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", file.Name(), err)
	}
	return boundedConfigData(data)
}
