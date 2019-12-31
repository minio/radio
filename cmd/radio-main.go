package cmd

import (
	"context"
	"net/http"
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

	for _, mcfg := range radio.rconfig.Mirror {
		cred, err := auth.CreateCredentials(mcfg.Local.AccessKey, mcfg.Local.SecretKey)
		if err != nil {
			logger.FatalIf(err, "Invalid credentials")
		}
		cred.SessionToken = mcfg.Local.SessionToken
		globalLocalCreds[mcfg.Local.AccessKey] = cred
	}

	// Disable logging until radio initialization is complete, any
	// error during initialization will be shown as a fatal message
	logger.Disable = true

	// Handle common command args.
	handleCommonCmdArgs(ctx)

	// Get port to listen on from radio address
	globalRadioHost, globalRadioPort = mustSplitHostPort(globalCLIContext.Addr)

	// On macOS, if a process already listens on LOCALIPADDR:PORT, net.Listen() falls back
	// to IPv6 address ie minio will start listening on IPv6 address whereas another
	// (non-)minio process is listening on IPv4 of given port.
	// To avoid this error situation we check for port availability.
	logger.FatalIf(checkPortAvailability(globalRadioHost, globalRadioPort), "Unable to start the radio")

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
	globalConsoleSys = NewConsoleLogger(context.Background(), globalEndpoints)

	if globalCacheConfig.Enabled {
		// initialize the new disk cache objects.
		var cacheAPI CacheObjectLayer
		cacheAPI, err = newServerCacheObjects(context.Background(), globalCacheConfig)
		logger.FatalIf(err, "Unable to initialize disk caching")

		globalObjLayerMutex.Lock()
		globalCacheObjectAPI = cacheAPI
		globalObjLayerMutex.Unlock()
	}

	// Override any values from ENVs.
	if err := lookupConfigEnv(radio.rconfig); err != nil {
		logger.FatalIf(err, "Unable to initialize server config")
	}

	router := mux.NewRouter().SkipClean(true)

	registerLockRESTHandlers(router, globalEndpoints)

	// Add healthcheck router
	registerHealthCheckRouter(router)

	// Add server metrics router
	registerMetricsRouter(router)

	for _, lCfg := range radio.rconfig.Mirror {
		registerAPIRouter(router, lCfg.Local.Bucket)
	}

	// If none of the routes match add default error handler routes
	router.NotFoundHandler = http.HandlerFunc(httpTraceAll(errorResponseHandler))
	router.MethodNotAllowedHandler = http.HandlerFunc(httpTraceAll(errorResponseHandler))

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
