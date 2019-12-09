package cmd

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"runtime"
	"strings"

	humanize "github.com/dustin/go-humanize"
	"github.com/minio/minio/pkg/color"
	xnet "github.com/minio/minio/pkg/net"
)

// Prints certificate expiry date warning
func getCertificateChainMsg(certs []*x509.Certificate) string {
	msg := color.Blue("\nCertificate expiry info:\n")
	totalCerts := len(certs)
	var expiringCerts int
	for i := totalCerts - 1; i >= 0; i-- {
		cert := certs[i]
		if cert.NotAfter.Before(UTCNow().Add(globalRadioCertExpireWarnDays)) {
			expiringCerts++
			msg += fmt.Sprintf(color.Bold("#%d %s will expire on %s\n"), expiringCerts, cert.Subject.CommonName, cert.NotAfter)
		}
	}
	if expiringCerts > 0 {
		return msg
	}
	return ""
}

// Prints the certificate expiry message.
func printCertificateMsg(certs []*x509.Certificate) {
	logStartupMessage(getCertificateChainMsg(certs))
}

func printCacheStorageInfo(storageInfo CacheStorageInfo) {
	msg := fmt.Sprintf("%s %s Free, %s Total", color.Blue("Cache Capacity:"),
		humanize.IBytes(uint64(storageInfo.Free)),
		humanize.IBytes(uint64(storageInfo.Total)))
	logStartupMessage(msg)
}

// Prints startup message for Object API acces, prints link to our SDK documentation.
func printObjectAPIMsg() {
	logStartupMessage(color.Blue("\nObject API (Amazon S3 compatible):"))
	logStartupMessage(color.Blue("   Go: ") + fmt.Sprintf(getFormatStr(len(goQuickStartGuide), 8), goQuickStartGuide))
	logStartupMessage(color.Blue("   Java: ") + fmt.Sprintf(getFormatStr(len(javaQuickStartGuide), 6), javaQuickStartGuide))
	logStartupMessage(color.Blue("   Python: ") + fmt.Sprintf(getFormatStr(len(pyQuickStartGuide), 4), pyQuickStartGuide))
	logStartupMessage(color.Blue("   JavaScript: ") + jsQuickStartGuide)
	logStartupMessage(color.Blue("   .NET: ") + fmt.Sprintf(getFormatStr(len(dotnetQuickStartGuide), 6), dotnetQuickStartGuide))
}

// Documentation links, these are part of message printing code.
const (
	mcQuickStartGuide      = "https://docs.min.io/docs/minio-client-quickstart-guide"
	mcAdminQuickStartGuide = "https://docs.min.io/docs/minio-admin-complete-guide.html"
	goQuickStartGuide      = "https://docs.min.io/docs/golang-client-quickstart-guide"
	jsQuickStartGuide      = "https://docs.min.io/docs/javascript-client-quickstart-guide"
	javaQuickStartGuide    = "https://docs.min.io/docs/java-client-quickstart-guide"
	pyQuickStartGuide      = "https://docs.min.io/docs/python-client-quickstart-guide"
	dotnetQuickStartGuide  = "https://docs.min.io/docs/dotnet-client-quickstart-guide"
)

// generates format string depending on the string length and padding.
func getFormatStr(strLen int, padding int) string {
	formatStr := fmt.Sprintf("%ds", strLen+padding)
	return "%" + formatStr
}

// Returns true if input is not IPv4, false if it is.
func isNotIPv4(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	ip := net.ParseIP(h)
	ok := ip.To4() != nil // This is always true of IP is IPv4

	// Returns true if input is not IPv4.
	return !ok
}

// strip api endpoints list with standard ports such as
// port "80" and "443" before displaying on the startup
// banner.  Returns a new list of API endpoints.
func stripStandardPorts(apiEndpoints []string) (newAPIEndpoints []string) {
	newAPIEndpoints = make([]string, len(apiEndpoints))
	// Check all API endpoints for standard ports and strip them.
	for i, apiEndpoint := range apiEndpoints {
		u, err := xnet.ParseHTTPURL(apiEndpoint)
		if err != nil {
			continue
		}
		if globalRadioHost == "" && isNotIPv4(u.Host) {
			// Skip all non-IPv4 endpoints when we bind to all interfaces.
			continue
		}
		newAPIEndpoints[i] = u.String()
	}
	return newAPIEndpoints
}

// Prints startup message for command line access. Prints link to our documentation
// and custom platform specific message.
func printCLIAccessMsg(endPoint string, alias string) {
	// Configure 'mc', following block prints platform specific information for minio client.
	if color.IsTerminal() {
		logStartupMessage(color.Blue("\nCommand-line Access: ") + mcQuickStartGuide)
		for _, cred := range globalLocalCreds {
			if runtime.GOOS == globalWindowsOSName {
				mcMessage := fmt.Sprintf("$ mc.exe config host add %s %s %s %s", alias,
					endPoint, cred.AccessKey, cred.SecretKey)
				logStartupMessage(fmt.Sprintf(getFormatStr(len(mcMessage), 3), mcMessage))
			} else {
				mcMessage := fmt.Sprintf("$ mc config host add %s %s %s %s", alias,
					endPoint, cred.AccessKey, cred.SecretKey)
				logStartupMessage(fmt.Sprintf(getFormatStr(len(mcMessage), 3), mcMessage))
			}
		}
	}
}

// Prints the formatted startup message.
func printRadioStartupMessage(apiEndPoints []string) {
	strippedAPIEndpoints := stripStandardPorts(apiEndPoints)
	// If cache layer is enabled, print cache capacity.
	cacheAPI := newCachedObjectLayerFn()
	if cacheAPI != nil {
		printCacheStorageInfo(cacheAPI.StorageInfo(context.Background()))
	}
	// Prints credential.
	printRadioCommonMsg(strippedAPIEndpoints)

	// Prints `mc` cli configuration message chooses
	// first endpoint as default.
	printCLIAccessMsg(strippedAPIEndpoints[0], "myradio")

	// Prints documentation message.
	printObjectAPIMsg()

	// SSL is configured reads certification chain, prints
	// authority and expiry.
	if globalIsSSL {
		printCertificateMsg(globalPublicCerts)
	}
}

// Prints common server startup message. Prints credential, region and browser access.
func printRadioCommonMsg(apiEndpoints []string) {
	apiEndpointStr := strings.Join(apiEndpoints, "  ")

	// Colorize the message and print.
	logStartupMessage(color.Blue("Endpoint: ") + color.Bold(fmt.Sprintf(getFormatStr(len(apiEndpointStr), 1), apiEndpointStr)))
}
