package config

// UI errors
var (
	ErrInvalidCacheDrivesValue = newErrFn(
		"Invalid cache drive value",
		"Please check the value in your config.yml",
		"Mounted drives or directories are wrong",
	)

	ErrInvalidCacheExcludesValue = newErrFn(
		"Invalid cache excludes value",
		"Please check the passed value in your config.yml",
		"Cache exclusion patterns are incorrect please refer to documentation",
	)

	ErrInvalidCacheExpiryValue = newErrFn(
		"Invalid cache expiry value",
		"Please check the passed value in your config.yml",
		"Valid cache expiry duration must be in days",
	)

	ErrInvalidCacheQuota = newErrFn(
		"Invalid cache quota value",
		"Please check the passed value in your config.yml",
		" Valid cache quota value must be between 0-100",
	)

	ErrInvalidCacheAfter = newErrFn(
		"Invalid cache after value",
		"Please check the passed value",
		"RADIO_CACHE_AFTER: Valid cache after value must be 0 or greater",
	)

	ErrInvalidCacheWatermarkLow = newErrFn(
		"Invalid cache low watermark value",
		"Please check the passed value",
		"RADIO_CACHE_WATERMARK_LOW: Valid cache low watermark value must be between 0-100",
	)

	ErrInvalidCacheWatermarkHigh = newErrFn(
		"Invalid cache high watermark value",
		"Please check the passed value",
		"RADIO_CACHE_WATERMARK_HIGH: Valid cache high watermark value must be between 0-100",
	)
	ErrInvalidAddressFlag = newErrFn(
		"--address input is invalid",
		"Please check --address parameter",
		`--address binds to a specific ADDRESS:PORT, ADDRESS can be an IPv4/IPv6 address or hostname (default port is ':9000')
	Examples: --address ':443'
		  --address '172.16.34.31:9000'
		  --address '[fe80::da00:a6c8:e3ae:ddd7]:9000'`,
	)

	ErrConfigInvalid = newErrFn(
		"--config input is invalid",
		"Please provide the correct path to your config",
		`--config or -c is path to your config.yml (default path is 'config.yml' in current directory)
        Examples: --config /etc/radio/config.yml
                  --config config.yml
                  --config /opt/radio/config.yml`,
	)

	ErrPortAlreadyInUse = newErrFn(
		"Port is already in use",
		"Please ensure no other program uses the same address/port",
		"",
	)

	ErrPortAccess = newErrFn(
		"Unable to use specified port",
		"Please ensure Radio binary has 'cap_net_bind_service=+ep' permissions",
		`Use 'sudo setcap cap_net_bind_service=+ep /path/to/radio' to provide sufficient permissions`,
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
		"Please set the password to environment variable `RADIO_CERT_PASSWD` so that the private key can be decrypted",
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
		"Please set the correct password in environment variable `RADIO_CERT_PASSWD`",
		"",
	)

	ErrInvalidRadioEndpoints = newErrFn(
		"Unable to initialize endpoint(s)",
		"Please check the specified value in your config.yml",
		"Please provide correct combination of remote servers",
	)
)
