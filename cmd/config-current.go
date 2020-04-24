package cmd

import (
	"fmt"

	"github.com/minio/radio/cmd/config"
	"github.com/minio/radio/cmd/logger"
)

func lookupConfigEnv(rconfig radioConfig) (err error) {
	globalServerRegion, err = config.LookupRegion()
	if err != nil {
		return fmt.Errorf("Invalid region configuration: %w", err)
	}

	// Enable console logging
	logger.AddTarget(globalConsoleSys.Console())

	return nil
}
