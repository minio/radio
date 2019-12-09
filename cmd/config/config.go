package config

import (
	"fmt"
	"regexp"

	"github.com/minio/minio/pkg/env"
)

// Error config error type
type Error struct {
	Kind ErrorKind
	Err  string
}

// ErrorKind config error kind
type ErrorKind int8

// Various error kinds.
const (
	ContinueKind ErrorKind = iota + 1
	SafeModeKind
)

// Errorf - formats according to a format specifier and returns
// the string as a value that satisfies error of type config.Error
func Errorf(errKind ErrorKind, format string, a ...interface{}) error {
	return Error{Kind: errKind, Err: fmt.Sprintf(format, a...)}
}

func (e Error) Error() string {
	return e.Err
}

var validRegionRegex = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9-_-]+$")

// LookupRegion - get current region.
func LookupRegion() (string, error) {
	region := env.Get(EnvRegion, "")
	if region == "" {
		region = env.Get(EnvRegionName, "")
	}
	if region != "" {
		if validRegionRegex.MatchString(region) {
			return region, nil
		}
		return "", Errorf(SafeModeKind,
			"region '%s' is invalid, expected simple characters such as [us-east-1, myregion...]",
			region)
	}
	return "", nil
}
