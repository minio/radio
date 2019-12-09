package cmd

// List of some generic handlers which are applied for all incoming requests.
var globalHandlers = []HandlerFunc{
	// set x-amz-request-id header.
	addCustomHeaders,
	// set HTTP security headers such as Content-Security-Policy.
	addSecurityHeaders,
	// Validate all the incoming requests.
	setRequestValidityHandler,
	// Network statistics
	setHTTPStatsHandler,
	// Limits all requests size to a maximum fixed limit
	setRequestSizeLimitHandler,
	// Limits all header sizes to a maximum fixed limit
	setRequestHeaderSizeLimitHandler,
	// Adds 'crossdomain.xml' policy handler to serve legacy flash clients.
	setCrossDomainPolicy,
	// Validates all incoming requests to have a valid date header.
	setTimeValidityHandler,
	// CORS setting for all browser API requests.
	setCorsHandler,
	// Auth handler verifies incoming authorization headers and
	// routes them accordingly. Client receives a HTTP error for
	// invalid/unsupported signatures.
	setAuthHandler,
	// Enforce rules specific for TLS requests
	setSSETLSHandler,
	// filters HTTP headers which are treated as metadata and are reserved
	// for internal use only.
	filterReservedMetadata,
	// Add new handlers here.
}
