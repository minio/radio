package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	"github.com/minio/cli"
	"github.com/minio/minio/pkg/auth"
	"github.com/minio/minio/pkg/certs"
	"github.com/minio/minio/pkg/color"
	"github.com/minio/minio/pkg/env"
	"github.com/minio/radio/cmd/config"
	xhttp "github.com/minio/radio/cmd/http"
	"github.com/minio/radio/cmd/logger"
)

func init() {
	logger.Init(GOPATH, GOROOT)
	logger.RegisterError(config.FmtError)
}

// radioFlags - server command specific flags
var radioFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "address",
		Value: ":" + globalRadioDefaultPort,
		Usage: "bind to a specific ADDRESS:PORT, ADDRESS can be an IP or hostname",
	},
	cli.StringFlag{
		Name:  "config, c",
		Usage: "path to radio configuration",
	},
}

var (
	radioCmd = cli.Command{
		Name:               "server",
		Usage:              "Start synchronous replication or erasure coding across object stores",
		Flags:              append(radioFlags, GlobalFlags...),
		Action:             radioMain,
		CustomHelpTemplate: radioTemplate,
	}
)

// startRadio - handler for 'radio server'.
func startRadio(ctx *cli.Context, radio *Radio) {
	if radio == nil {
		logger.FatalIf(errUnexpected, "Radio implementation not initialized")
	}

	for _, cfg := range radio.rconfig.Buckets {
		cred, err := auth.CreateCredentials(cfg.AccessKey, cfg.SecretKey)
		if err != nil {
			logger.FatalIf(err, "Invalid credentials")
		}
		globalLocalCreds[cfg.AccessKey] = cred
	}

	// Disable logging until radio initialization is complete, any
	// error during initialization will be shown as a fatal message
	logger.Disable = true

	// Check and load TLS certificates.
	var err error
	globalPublicCerts, globalTLSCerts, globalIsSSL, err = getTLSConfig(radio.rconfig)
	logger.FatalIf(err, "Invalid TLS certificate file")

	// Check and load Root CAs.
	globalRootCAs, err = config.GetRootCAs(radio.rconfig.Distribute.Certs.CAPath)
	logger.FatalIf(err, "Failed to read root CAs (%v)", err)

	// Set system resources to maximum.
	logger.LogIf(context.Background(), setMaxResources())

	// Initialize globalConsoleSys system
	globalConsoleSys = NewConsoleLogger(context.Background())

	// Override any values from ENVs.
	if err := lookupConfigEnv(radio.rconfig); err != nil {
		logger.FatalIf(err, "Unable to initialize server config")
	}

	if globalCacheConfig.Enabled {
		// initialize the new disk cache objects.
		var cacheAPI CacheObjectLayer
		cacheAPI, err = newServerCacheObjects(context.Background(), globalCacheConfig)
		logger.FatalIf(err, "Unable to initialize disk caching")

		globalObjLayerMutex.Lock()
		globalCacheObjectAPI = cacheAPI
		globalObjLayerMutex.Unlock()
	}

	router := mux.NewRouter().SkipClean(true).UseEncodedPath()

	// If none of the routes match add default error handler routes
	router.NotFoundHandler = collectAPIStats("notfound", httpTraceAll(errorResponseHandler))
	router.MethodNotAllowedHandler = collectAPIStats("methodnotallowed", httpTraceAll(errorResponseHandler))

	if len(radio.endpoints) > 0 {
		registerLockRESTHandlers(router, radio.endpoints, radio.rconfig.Distribute.Token)
	}

	// Add healthcheck router
	registerHealthCheckRouter(router)

	// Add server metrics router
	registerMetricsRouter(router)

	for bucket := range radio.rconfig.Buckets {
		registerAPIRouter(router, bucket)
	}

	var getCert certs.GetCertificateFunc
	if globalTLSCerts != nil {
		getCert = globalTLSCerts.GetCertificate
	}

	httpServer := xhttp.NewServer([]string{globalCLIContext.Addr},
		criticalErrorHandler{registerHandlers(router, globalHandlers...)}, getCert)
	go func() {
		globalHTTPServerErrorCh <- httpServer.Start()
	}()

	globalObjLayerMutex.Lock()
	globalHTTPServer = httpServer
	globalObjLayerMutex.Unlock()

	signal.Notify(globalOSSignalCh, os.Interrupt, syscall.SIGTERM)

	newObject, err := radio.NewRadioLayer()
	if err != nil {
		// Stop watching for any certificate changes.
		globalTLSCerts.Stop()

		globalHTTPServer.Shutdown()
		logger.FatalIf(err, "Unable to initialize radio backend")
	}

	// Re-enable logging
	logger.Disable = false

	// Once endpoints are finalized, initialize the new object api in safe mode.
	globalObjLayerMutex.Lock()
	globalObjectAPI = newObject
	globalObjLayerMutex.Unlock()

	// This is only to uniquely identify each radio deployments.
	globalDeploymentID = env.Get("RADIO_DEPLOYMENT_ID", mustGetUUID())
	logger.SetDeploymentID(globalDeploymentID)

	// Prints the formatted startup message once object layer is initialized.
	if !globalCLIContext.Quiet {
		// Print a warning message if radio is not ready for production before the startup banner.
		if !radio.Production() {
			logStartupMessage(color.Yellow("               *** Warning: Not Ready for Production ***"))
		}

		// Print radio startup message.
		printRadioStartupMessage(getAPIEndpoints())
	}

	handleSignals()
}
