package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/desktopgame/backup/internal/config"
)

// Open connects to the backend described by the configuration.
func Open(ctx context.Context, cfg config.Storage) (Storage, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case config.StorageLocal:
		return NewLocal(cfg.Path)
	case config.StorageFTP:
		return NewFTP(ctx, FTPOptions{
			Host:     cfg.Host,
			Port:     cfg.Port,
			Username: cfg.Username,
			Password: cfg.Password,
			Path:     cfg.Path,
		})
	default:
		return nil, fmt.Errorf("unsupported storage type %q", cfg.Type)
	}
}
