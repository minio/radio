package cmd

import (
	"errors"
)

const (
	jwtAlgorithm = "Bearer"
)

var errAuthentication = errors.New("Authentication failed, check your access credentials")
