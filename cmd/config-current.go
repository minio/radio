package cmd

import (
	"fmt"

	"github.com/minio/radio/cmd/config"
	"github.com/minio/radio/cmd/config/cache"
	"github.com/minio/radio/cmd/logger"
)

const minioConfigPrefix = "config"

func lookupConfigEnv() (err error) {
	globalServerRegion, err = config.LookupRegion()
	if err != nil {
		return fmt.Errorf("Invalid region configuration: %w", err)
	}

	globalCacheConfig, err = cache.LookupConfig()
	if err != nil {
		return fmt.Errorf("Unable to setup cache: %w", err)
	}

	// Enable console logging
	logger.AddTarget(globalConsoleSys.Console())

	return nil
}
