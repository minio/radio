package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/minio/radio/cmd/logger"
)

func handleSignals() {
	// Custom exit function
	exit := func(success bool) {
		// If global profiler is set stop before we exit.
		if globalProfiler != nil {
			globalProfiler.Stop()
		}

		if success {
			os.Exit(0)
		}

		os.Exit(1)
	}

	stopProcess := func() bool {
		var err, oerr error

		// Stop watching for any certificate changes.
		globalTLSCerts.Stop()

		if httpServer := newHTTPServerFn(); httpServer != nil {
			err = httpServer.Shutdown()
			logger.LogIf(context.Background(), err)
		}

		// send signal to various go-routines that they need to quit.
		close(GlobalServiceDoneCh)

		return (err == nil && oerr == nil)
	}

	for {
		select {
		case err := <-globalHTTPServerErrorCh:
			if err != nil {
				logger.Fatal(err, "Unable to start Radio server")
			}
			exit(true)
		case osSignal := <-globalOSSignalCh:
			logger.Info("Exiting on signal: %s", strings.ToUpper(osSignal.String()))
			exit(stopProcess())
		case signal := <-globalServiceSignalCh:
			switch signal {
			case serviceRestart:
				logger.Info("Restarting on service signal")
				stop := stopProcess()
				rerr := restartProcess()
				logger.LogIf(context.Background(), rerr)
				exit(stop && rerr == nil)
			case serviceStop:
				logger.Info("Stopping on service signal")
				exit(stopProcess())
			}
		}
	}
}
