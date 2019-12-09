package config

// UI errors
var (
	ErrInvalidCacheDrivesValue = newErrFn(
		"Invalid cache drive value",
		"Please check the value in this ENV variable",
		"MINIO_CACHE_DRIVES: Mounted drives or directories are delimited by `,`",
	)

	ErrInvalidCacheExcludesValue = newErrFn(
		"Invalid cache excludes value",
		"Please check the passed value",
		"MINIO_CACHE_EXCLUDE: Cache exclusion patterns are delimited by `,`",
	)

	ErrInvalidCacheExpiryValue = newErrFn(
		"Invalid cache expiry value",
		"Please check the passed value",
		"MINIO_CACHE_EXPIRY: Valid cache expiry duration must be in days",
	)

	ErrInvalidCacheQuota = newErrFn(
		"Invalid cache quota value",
		"Please check the passed value",
		"MINIO_CACHE_QUOTA: Valid cache quota value must be between 0-100",
	)

	ErrInvalidCacheEncryptionKey = newErrFn(
		"Invalid cache encryption master key value",
		"Please check the passed value",
		"MINIO_CACHE_ENCRYPTION_MASTER_KEY: For more information, please refer to https://docs.min.io/docs/minio-disk-cache-guide",
	)

	ErrUnexpectedBackendVersion = newErrFn(
		"Backend version seems to be too recent",
		"Please update to the latest MinIO version",
		"",
	)

	ErrInvalidAddressFlag = newErrFn(
		"--address input is invalid",
		"Please check --address parameter",
		`--address binds to a specific ADDRESS:PORT, ADDRESS can be an IPv4/IPv6 address or hostname (default port is ':9000')
	Examples: --address ':443'
		  --address '172.16.34.31:9000'
		  --address '[fe80::da00:a6c8:e3ae:ddd7]:9000'`,
	)

	ErrUnableToWriteInBackend = newErrFn(
		"Unable to write to the backend",
		"Please ensure MinIO binary has write permissions for the backend",
		`Verify if MinIO binary is running as the same user who has write permissions for the backend`,
	)

	ErrPortAlreadyInUse = newErrFn(
		"Port is already in use",
		"Please ensure no other program uses the same address/port",
		"",
	)

	ErrPortAccess = newErrFn(
		"Unable to use specified port",
		"Please ensure MinIO binary has 'cap_net_bind_service=+ep' permissions",
		`Use 'sudo setcap cap_net_bind_service=+ep /path/to/minio' to provide sufficient permissions`,
	)

	ErrNoPermissionsToAccessDirFiles = newErrFn(
		"Missing permissions to access the specified path",
		"Please ensure the specified path can be accessed",
		"",
	)

	ErrSSLUnexpectedError = newErrFn(
		"Invalid TLS certificate",
		"Please check the content of your certificate data",
		`Only PEM (x.509) format is accepted as valid public & private certificates`,
	)

	ErrSSLUnexpectedData = newErrFn(
		"Invalid TLS certificate",
		"Please check your certificate",
		"",
	)

	ErrSSLNoPassword = newErrFn(
		"Missing TLS password",
		"Please set the password to environment variable `MINIO_CERT_PASSWD` so that the private key can be decrypted",
		"",
	)

	ErrNoCertsAndHTTPSEndpoints = newErrFn(
		"HTTPS specified in endpoints, but no TLS certificate is found on the local machine",
		"Please add TLS certificate or use HTTP endpoints only",
		"Refer to https://docs.min.io/docs/how-to-secure-access-to-minio-server-with-tls for information about how to load a TLS certificate in your server",
	)

	ErrCertsAndHTTPEndpoints = newErrFn(
		"HTTP specified in endpoints, but the server in the local machine is configured with a TLS certificate",
		"Please remove the certificate in the configuration directory or switch to HTTPS",
		"",
	)

	ErrSSLWrongPassword = newErrFn(
		"Unable to decrypt the private key using the provided password",
		"Please set the correct password in environment variable `MINIO_CERT_PASSWD`",
		"",
	)

	ErrUnexpectedDataContent = newErrFn(
		"Unexpected data content",
		"Please contact MinIO at https://slack.min.io",
		"",
	)

	ErrUnexpectedError = newErrFn(
		"Unexpected error",
		"Please contact MinIO at https://slack.min.io",
		"",
	)
)
