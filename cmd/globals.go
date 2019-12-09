package cmd

import (
	"crypto/x509"
	"os"
	"time"

	"github.com/minio/minio-go/v6/pkg/set"

	humanize "github.com/dustin/go-humanize"
	xhttp "github.com/minio/minio/cmd/http"
	"github.com/minio/minio/pkg/auth"
	"github.com/minio/minio/pkg/certs"
	"github.com/minio/minio/pkg/pubsub"
	"github.com/minio/radio/cmd/config/cache"
)

// minio configuration related constants.
const (
	globalRadioCertExpireWarnDays = time.Hour * 24 * 30 // 30 days.

	globalRadioDefaultPort = "9000"

	globalRadioDefaultRegion = ""
	// This is a sha256 output of ``arn:aws:iam::minio:user/admin``,
	// this is kept in present form to be compatible with S3 owner ID
	// requirements -
	//
	// ```
	//    The canonical user ID is the Amazon S3–only concept.
	//    It is 64-character obfuscated version of the account ID.
	// ```
	// http://docs.aws.amazon.com/AmazonS3/latest/dev/example-walkthroughs-managing-access-example4.html
	globalRadioDefaultOwnerID      = "02d6176db174dc93cb1b899f7c6078f08654445fe8cf1b6ce98d8855f66bdbf4"
	globalRadioDefaultStorageClass = "STANDARD"
	globalWindowsOSName            = "windows"
	globalNetBSDOSName             = "netbsd"
	globalMacOSName                = "darwin"
	globalRadioMode                = "mode-radio-1"

	// Add new global values here.
)

const (
	// Limit fields size (except file) to 1Mib since Policy document
	// can reach that size according to https://aws.amazon.com/articles/1434
	maxFormFieldSize = int64(1 * humanize.MiByte)

	// Limit memory allocation to store multipart data
	maxFormMemory = int64(5 * humanize.MiByte)

	// The maximum allowed time difference between the incoming request
	// date and server date during signature verification.
	globalMaxSkewTime = 15 * time.Minute // 15 minutes skew allowed.

	// Refresh interval to update in-memory iam config cache.
	globalRefreshIAMInterval = 5 * time.Minute

	// Limit of location constraint XML for unauthenticted PUT bucket operations.
	maxLocationConstraintSize = 3 * humanize.MiByte
)

var globalCLIContext = struct {
	JSON, Quiet    bool
	Anonymous      bool
	Addr           string
	StrictS3Compat bool
}{}

var (
	// This flag is set to 'us-east-1' by default
	globalServerRegion = globalRadioDefaultRegion

	// MinIO local server address (in `host:port` format)
	globalRadioAddr = ""
	// MinIO default port, can be changed through command line.
	globalRadioPort = globalRadioDefaultPort
	// Holds the host that was passed using --address
	globalRadioHost = ""

	// CA root certificates, a nil value means system certs pool will be used
	globalRootCAs *x509.CertPool

	// IsSSL indicates if the server is configured with SSL.
	globalIsSSL bool

	globalTLSCerts *certs.Certs

	globalHTTPServer        *xhttp.Server
	globalHTTPServerErrorCh = make(chan error)
	globalOSSignalCh        = make(chan os.Signal, 1)

	// global Trace system to send HTTP request/response logs to
	// registered listeners
	globalHTTPTrace = pubsub.New()

	// global console system to send console logs to
	// registered listeners
	globalConsoleSys *HTTPConsoleLoggerSys

	globalEndpoints EndpointZones

	// Global server's network statistics
	globalConnStats = newConnStats()

	// Global HTTP request statisitics
	globalHTTPStats = newHTTPStats()

	// Time when object layer was initialized on start up.
	globalBootTime time.Time

	globalLocalCreds = map[string]auth.Credentials{}

	globalPublicCerts []*x509.Certificate

	globalDomainNames []string      // Root domains for virtual host style requests
	globalDomainIPs   set.StringSet // Root domain IP address(s) for a distributed MinIO deployment

	globalObjectTimeout    = newDynamicTimeout( /*1*/ 10*time.Minute /*10*/, 600*time.Second) // timeout for Object API related ops
	globalOperationTimeout = newDynamicTimeout(10*time.Minute /*30*/, 600*time.Second)        // default timeout for general ops

	// Disk cache drives
	globalCacheConfig cache.Config

	// Deployment ID - unique per deployment
	globalDeploymentID string

	// Add new variable global values here.
)
