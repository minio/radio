package cmd

import (
	"os"
	"path/filepath"

	homedir "github.com/mitchellh/go-homedir"
)

const (
	// Default minio configuration directory where below configuration files/directories are stored.
	defaultMinioCertsDir = ".minio"

	// Directory contains below files/directories for HTTPS configuration.
	certsDir = "certs"

	// Directory contains all CA certificates other than system defaults for HTTPS.
	certsCADir = "CAs"

	// Public certificate file for HTTPS.
	publicCertFile = "public.crt"

	// Private key file for HTTPS.
	privateKeyFile = "private.key"
)

// CertsDir - points to a user set directory.
type CertsDir struct {
	path string
}

func getDefaultCertsDir() string {
	homeDir, err := homedir.Dir()
	if err != nil {
		return ""
	}

	return filepath.Join(homeDir, defaultMinioCertsDir)
}

func getDefaultCertsCADir() string {
	return filepath.Join(getDefaultCertsDir(), certsCADir)
}

var (
	// Default config, certs and CA directories.
	defaultCertsDir   = &CertsDir{path: getDefaultCertsDir()}
	defaultCertsCADir = &CertsDir{path: getDefaultCertsCADir()}

	// Points to current configuration directory -- deprecated, to be removed in future.
	globalCertsDir = defaultCertsDir

	// Points to relative path to certs directory and is <value-of-certs-dir>/CAs
	globalCertsCADir = defaultCertsCADir
)

// Get - returns current directory.
func (dir *CertsDir) Get() string {
	return dir.path
}

// Attempts to create all directories, ignores any permission denied errors.
func mkdirAllIgnorePerm(path string) error {
	err := os.MkdirAll(path, 0700)
	if err != nil {
		// It is possible in kubernetes like deployments this directory
		// is already mounted and is not writable, ignore any write errors.
		if os.IsPermission(err) {
			err = nil
		}
	}
	return err
}

func getPublicCertFile() string {
	return filepath.Join(globalCertsDir.Get(), publicCertFile)
}

func getPrivateKeyFile() string {
	return filepath.Join(globalCertsDir.Get(), privateKeyFile)
}
