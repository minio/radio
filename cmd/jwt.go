package cmd

import (
	"errors"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	"github.com/minio/minio/pkg/auth"
)

const (
	jwtAlgorithm = "Bearer"

	// Default JWT token for web handlers is one day.
	defaultJWTExpiry = 24 * time.Hour

	// Inter-node JWT token expiry is 100 years approx.
	defaultInterNodeJWTExpiry = 100 * 365 * 24 * time.Hour

	// URL JWT token expiry is one minute (might be exposed).
	defaultURLJWTExpiry = time.Minute
)

var (
	errInvalidAccessKeyID   = errors.New("The access key ID you provided does not exist in our records")
	errChangeCredNotAllowed = errors.New("Changing access key and secret key not allowed")
	errAuthentication       = errors.New("Authentication failed, check your access credentials")
	errNoAuthToken          = errors.New("JWT token missing")
	errIncorrectCreds       = errors.New("Current access key or secret key is incorrect")
)

func authenticateJWTUsers(accessKey, secretKey string, expiry time.Duration) (string, error) {
	passedCredential, err := auth.CreateCredentials(accessKey, secretKey)
	if err != nil {
		return "", err
	}
	expiresAt := UTCNow().Add(expiry)
	return authenticateJWTUsersWithCredentials(passedCredential, expiresAt)
}

func authenticateJWTUsersWithCredentials(credentials auth.Credentials, expiresAt time.Time) (string, error) {
	serverCred, ok := globalLocalCreds[credentials.AccessKey]
	if !ok {
		return "", errInvalidAccessKeyID
	}

	if !serverCred.Equal(credentials) {
		return "", errAuthentication
	}

	claims := jwtgo.MapClaims{}
	claims["exp"] = expiresAt.Unix()
	claims["sub"] = credentials.AccessKey
	claims["accessKey"] = credentials.AccessKey

	jwt := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims)
	return jwt.SignedString([]byte(serverCred.SecretKey))
}

func authenticateJWTAdmin(accessKey, secretKey string, expiry time.Duration) (string, error) {
	passedCredential, err := auth.CreateCredentials(accessKey, secretKey)
	if err != nil {
		return "", err
	}

	serverCred, ok := globalLocalCreds[passedCredential.AccessKey]
	if !ok {
		return "", errInvalidAccessKeyID
	}

	if !serverCred.Equal(passedCredential) {
		return "", errAuthentication
	}

	claims := jwtgo.MapClaims{}
	claims["exp"] = UTCNow().Add(expiry).Unix()
	claims["sub"] = passedCredential.AccessKey
	claims["accessKey"] = passedCredential.AccessKey

	jwt := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims)
	return jwt.SignedString([]byte(serverCred.SecretKey))
}

func authenticateNode(accessKey, secretKey string) (string, error) {
	return authenticateJWTAdmin(accessKey, secretKey, defaultInterNodeJWTExpiry)
}

func authenticateWeb(accessKey, secretKey string) (string, error) {
	return authenticateJWTUsers(accessKey, secretKey, defaultJWTExpiry)
}

func authenticateURL(accessKey, secretKey string) (string, error) {
	return authenticateJWTUsers(accessKey, secretKey, defaultURLJWTExpiry)
}

type mapClaims struct {
	jwtgo.MapClaims
}

func (m mapClaims) Map() map[string]interface{} {
	return m.MapClaims
}

func (m mapClaims) AccessKey() string {
	claimSub, ok := m.MapClaims["accessKey"].(string)
	if !ok {
		claimSub, _ = m.MapClaims["sub"].(string)
	}
	return claimSub
}

func newAuthToken() string {
	return envTokenValue
}
