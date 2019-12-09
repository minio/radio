package cache

import (
	"errors"
	"strconv"

	"github.com/minio/minio/pkg/env"
	"github.com/minio/radio/cmd/config"
)

// Cache ENVs
const (
	Drives  = "drives"
	Exclude = "exclude"
	Expiry  = "expiry"
	MaxUse  = "maxuse"
	Quota   = "quota"

	EnvCacheDrives              = "MINIO_CACHE_DRIVES"
	EnvCacheExclude             = "MINIO_CACHE_EXCLUDE"
	EnvCacheExpiry              = "MINIO_CACHE_EXPIRY"
	EnvCacheMaxUse              = "MINIO_CACHE_MAXUSE"
	EnvCacheQuota               = "MINIO_CACHE_QUOTA"
	EnvCacheEncryptionMasterKey = "MINIO_CACHE_ENCRYPTION_MASTER_KEY"

	DefaultExpiry = "90"
	DefaultQuota  = "80"
)

const (
	cacheDelimiter = ","
)

// LookupConfig - extracts cache configuration provided by environment
// variables and merge them with provided CacheConfiguration.
func LookupConfig() (Config, error) {
	cfg := Config{}

	drives := env.Get(EnvCacheDrives, "")
	if len(drives) == 0 {
		return cfg, nil
	}

	var err error
	cfg.Drives, err = parseCacheDrives(drives)
	if err != nil {
		return cfg, err
	}

	cfg.Enabled = true
	if excludes := env.Get(EnvCacheExclude, ""); excludes != "" {
		cfg.Exclude, err = parseCacheExcludes(excludes)
		if err != nil {
			return cfg, err
		}
	}

	if expiryStr := env.Get(EnvCacheExpiry, DefaultExpiry); expiryStr != "" {
		cfg.Expiry, err = strconv.Atoi(expiryStr)
		if err != nil {
			return cfg, config.ErrInvalidCacheExpiryValue(err)
		}
	}

	if maxUseStr := env.Get(EnvCacheMaxUse, DefaultQuota); maxUseStr != "" {
		cfg.MaxUse, err = strconv.Atoi(maxUseStr)
		if err != nil {
			return cfg, config.ErrInvalidCacheQuota(err)
		}
		// maxUse should be a valid percentage.
		if cfg.MaxUse < 0 || cfg.MaxUse > 100 {
			err := errors.New("config max use value should not be null or negative")
			return cfg, config.ErrInvalidCacheQuota(err)
		}
		cfg.Quota = cfg.MaxUse
	}

	if quotaStr := env.Get(EnvCacheQuota, DefaultQuota); quotaStr != "" {
		cfg.Quota, err = strconv.Atoi(quotaStr)
		if err != nil {
			return cfg, config.ErrInvalidCacheQuota(err)
		}
		// quota should be a valid percentage.
		if cfg.Quota < 0 || cfg.Quota > 100 {
			err := errors.New("config quota value should not be null or negative")
			return cfg, config.ErrInvalidCacheQuota(err)
		}
		cfg.MaxUse = cfg.Quota
	}

	return cfg, nil
}
