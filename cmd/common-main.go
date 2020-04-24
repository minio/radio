package cmd

import (
	"crypto/x509"

	"github.com/minio/cli"
	"github.com/minio/minio/pkg/certs"
	"github.com/minio/radio/cmd/config"
	"github.com/minio/radio/cmd/logger"
)

func handleCommonCmdArgs(ctx *cli.Context) {
	// Get "json" flag from command line argument and
	// enable json and quite modes if json flag is turned on.
	globalCLIContext.JSON = ctx.IsSet("json") || ctx.GlobalIsSet("json")
	if globalCLIContext.JSON {
		logger.EnableJSON()
	}

	// Get quiet flag from command line argument.
	globalCLIContext.Quiet = ctx.IsSet("quiet") || ctx.GlobalIsSet("quiet")
	if globalCLIContext.Quiet {
		logger.EnableQuiet()
	}

	// Get anonymous flag from command line argument.
	globalCLIContext.Anonymous = ctx.IsSet("anonymous") || ctx.GlobalIsSet("anonymous")
	if globalCLIContext.Anonymous {
		logger.EnableAnonymous()
	}

	// Fetch address option
	globalCLIContext.Addr = ctx.GlobalString("address")
	if globalCLIContext.Addr == "" || globalCLIContext.Addr == ":"+globalRadioDefaultPort {
		globalCLIContext.Addr = ctx.String("address")
	}

	// Check "compat" flag from command line argument.
	globalCLIContext.StrictS3Compat = ctx.IsSet("compat") || ctx.GlobalIsSet("compat")
}

func logStartupMessage(msg string) {
	if globalConsoleSys != nil {
		globalConsoleSys.Send(msg, string(logger.All))
	}
	logger.StartupMessage(msg)
}

func getTLSConfig(rconfig radioConfig) (x509Certs []*x509.Certificate, c *certs.Certs, secureConn bool, err error) {
	certFile := rconfig.Certs.CertFile
	keyFile := rconfig.Certs.KeyFile
	if !(isFile(certFile) && isFile(keyFile)) {
		return nil, nil, false, nil
	}

	if x509Certs, err = config.ParsePublicCertFile(certFile); err != nil {
		return nil, nil, false, err
	}

	c, err = certs.New(certFile, keyFile, config.LoadX509KeyPair)
	if err != nil {
		return nil, nil, false, err
	}

	secureConn = true
	return x509Certs, c, secureConn, nil
}
