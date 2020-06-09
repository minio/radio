package cmd

import (
	"fmt"

	"github.com/minio/radio/cmd/config"
	"github.com/minio/radio/cmd/config/cache"
	"github.com/minio/radio/cmd/logger"
)

func lookupConfigEnv(rconfig radioConfig) (err error) {
	globalServerRegion, err = config.LookupRegion()
	if err != nil {
		return fmt.Errorf("Invalid region configuration: %w", err)
	}
	globalCacheConfig, err = cache.LookupConfig()
	if err != nil {
		return fmt.Errorf("Unable to setup cache: %w", err)
	}

	if !globalCacheConfig.Enabled {
		globalCacheConfig.Drives = rconfig.Cache.Drives
		globalCacheConfig.Exclude = rconfig.Cache.Exclude
		globalCacheConfig.Quota = rconfig.Cache.Quota
		globalCacheConfig.Expiry = rconfig.Cache.Expiry
		globalCacheConfig.Enabled = len(rconfig.Cache.Drives) > 0
	}

	// Enable console logging
	logger.AddTarget(globalConsoleSys.Console())

	return nil
}
