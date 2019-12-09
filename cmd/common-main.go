package cmd

import (
	"crypto/x509"
	"errors"
	"path/filepath"

	"github.com/minio/cli"
	"github.com/minio/minio/cmd/config"
	"github.com/minio/minio/pkg/certs"
	"github.com/minio/radio/cmd/logger"
)

func newCertsDirFromCtx(ctx *cli.Context, option string, getDefaultDir func() string) *CertsDir {
	var dir string

	switch {
	case ctx.IsSet(option):
		dir = ctx.String(option)
	case ctx.GlobalIsSet(option):
		dir = ctx.GlobalString(option)
		// cli package does not expose parent's option option.  Below code is workaround.
		if dir == "" || dir == getDefaultDir() {
			if ctx.Parent().GlobalIsSet(option) {
				dir = ctx.Parent().GlobalString(option)
			}
		}
	default:
		// Neither local nor global option is provided.  In this case, try to use
		// default directory.
		dir = getDefaultDir()
		if dir == "" {
			logger.FatalIf(errInvalidArgument, "%s option must be provided", option)
		}
	}

	if dir == "" {
		logger.FatalIf(errors.New("empty directory"), "%s directory cannot be empty", option)
	}

	// Disallow relative paths, figure out absolute paths.
	dirAbs, err := filepath.Abs(dir)
	logger.FatalIf(err, "Unable to fetch absolute path for %s=%s", option, dir)

	logger.FatalIf(mkdirAllIgnorePerm(dirAbs), "Unable to create directory specified %s=%s", option, dir)

	return &CertsDir{path: dirAbs}
}

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

	// Set all config, certs and CAs directories.
	globalCertsDir = newCertsDirFromCtx(ctx, "certs-dir", defaultCertsDir.Get)
	globalCertsCADir = &CertsDir{path: filepath.Join(globalCertsDir.Get(), certsCADir)}

	logger.FatalIf(mkdirAllIgnorePerm(globalCertsCADir.Get()), "Unable to create certs CA directory at %s", globalCertsCADir.Get())

	// Check "compat" flag from command line argument.
	globalCLIContext.StrictS3Compat = ctx.IsSet("compat") || ctx.GlobalIsSet("compat")
}

func logStartupMessage(msg string) {
	if globalConsoleSys != nil {
		globalConsoleSys.Send(msg, string(logger.All))
	}
	logger.StartupMessage(msg)
}

func getTLSConfig() (x509Certs []*x509.Certificate, c *certs.Certs, secureConn bool, err error) {
	if !(isFile(getPublicCertFile()) && isFile(getPrivateKeyFile())) {
		return nil, nil, false, nil
	}

	if x509Certs, err = config.ParsePublicCertFile(getPublicCertFile()); err != nil {
		return nil, nil, false, err
	}

	c, err = certs.New(getPublicCertFile(), getPrivateKeyFile(), config.LoadX509KeyPair)
	if err != nil {
		return nil, nil, false, err
	}

	secureConn = true
	return x509Certs, c, secureConn, nil
}
