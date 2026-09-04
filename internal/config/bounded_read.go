package config

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const MaxEffectiveConfigSize int64 = 4 << 20

var ErrConfigTooLarge = errors.New("config file exceeds maximum size")

func boundedConfigData(data []byte) ([]byte, error) {
	if int64(len(data)) > MaxEffectiveConfigSize {
		return nil, fmt.Errorf("%w: maximum %d bytes", ErrConfigTooLarge, MaxEffectiveConfigSize)
	}
	return append([]byte(nil), data...), nil
}

func readConfigFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	limited := io.LimitReader(file, MaxEffectiveConfigSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	return boundedConfigData(data)
}
