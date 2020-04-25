package cache

import (
	"errors"
	"strconv"

	"github.com/minio/minio/pkg/env"
	"github.com/minio/radio/cmd/config"
)

// Cache ENVs
const (
	Drives        = "drives"
	Exclude       = "exclude"
	Expiry        = "expiry"
	MaxUse        = "maxuse"
	Quota         = "quota"
	After         = "after"
	WatermarkLow  = "watermark_low"
	WatermarkHigh = "watermark_high"

	EnvCacheDrives        = "RADIO_CACHE_DRIVES"
	EnvCacheExclude       = "RADIO_CACHE_EXCLUDE"
	EnvCacheExpiry        = "RADIO_CACHE_EXPIRY"
	EnvCacheMaxUse        = "RADIO_CACHE_MAXUSE"
	EnvCacheQuota         = "RADIO_CACHE_QUOTA"
	EnvCacheAfter         = "RADIO_CACHE_AFTER"
	EnvCacheWatermarkLow  = "RADIO_CACHE_WATERMARK_LOW"
	EnvCacheWatermarkHigh = "RADIO_CACHE_WATERMARK_HIGH"

	DefaultExpiry        = "90"
	DefaultQuota         = "80"
	DefaultAfter         = "0"
	DefaultWaterMarkLow  = "70"
	DefaultWaterMarkHigh = "80"
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

	if afterStr := env.Get(EnvCacheAfter, DefaultAfter); afterStr != "" {
		cfg.After, err = strconv.Atoi(afterStr)
		if err != nil {
			return cfg, config.ErrInvalidCacheAfter(err)
		}
		// after should be a valid value >= 0.
		if cfg.After < 0 {
			err := errors.New("cache after value cannot be less than 0")
			return cfg, config.ErrInvalidCacheAfter(err)
		}
	}

	if lowWMStr := env.Get(EnvCacheWatermarkLow, DefaultWaterMarkLow); lowWMStr != "" {
		cfg.WatermarkLow, err = strconv.Atoi(lowWMStr)
		if err != nil {
			return cfg, config.ErrInvalidCacheWatermarkLow(err)
		}
		// WatermarkLow should be a valid percentage.
		if cfg.WatermarkLow < 0 || cfg.WatermarkLow > 100 {
			err := errors.New("config min watermark value should be between 0 and 100")
			return cfg, config.ErrInvalidCacheWatermarkLow(err)
		}
	}

	if highWMStr := env.Get(EnvCacheWatermarkHigh, DefaultWaterMarkHigh); highWMStr != "" {
		cfg.WatermarkHigh, err = strconv.Atoi(highWMStr)
		if err != nil {
			return cfg, config.ErrInvalidCacheWatermarkHigh(err)
		}

		// MaxWatermark should be a valid percentage.
		if cfg.WatermarkHigh < 0 || cfg.WatermarkHigh > 100 {
			err := errors.New("config high watermark value should be between 0 and 100")
			return cfg, config.ErrInvalidCacheWatermarkHigh(err)
		}
	}
	if cfg.WatermarkLow > cfg.WatermarkHigh {
		err := errors.New("config high watermark value should be greater than low watermark value")
		return cfg, config.ErrInvalidCacheWatermarkHigh(err)
	}
	return cfg, nil
}
