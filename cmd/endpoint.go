package cmd

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v6/pkg/set"
	"github.com/minio/minio/pkg/ellipses"
	"github.com/minio/radio/cmd/config"
)

// EndpointType - enum for endpoint type.
type EndpointType int

const (
	// PathEndpointType - path style endpoint type enum.
	PathEndpointType EndpointType = iota + 1

	// URLEndpointType - URL style endpoint type enum.
	URLEndpointType

	retryInterval = 5 // In Seconds.
)

// Endpoint - any type of endpoint.
type Endpoint struct {
	*url.URL
	IsLocal  bool
	SetIndex int
}

func (endpoint Endpoint) String() string {
	if endpoint.Host == "" {
		return endpoint.Path
	}

	return endpoint.URL.String()
}

// Type - returns type of endpoint.
func (endpoint Endpoint) Type() EndpointType {
	if endpoint.Host == "" {
		return PathEndpointType
	}

	return URLEndpointType
}

// HTTPS - returns true if secure for URLEndpointType.
func (endpoint Endpoint) HTTPS() bool {
	return endpoint.Scheme == "https"
}

// UpdateIsLocal - resolves the host and updates if it is local or not.
func (endpoint *Endpoint) UpdateIsLocal() (err error) {
	if !endpoint.IsLocal {
		endpoint.IsLocal, err = isLocalHost(endpoint.Hostname(), endpoint.Port(), globalRadioPort)
		if err != nil {
			return err
		}
	}
	return nil
}

// NewEndpoint - returns new endpoint based on given arguments.
func NewEndpoint(arg string) (ep Endpoint, e error) {
	// isEmptyPath - check whether given path is not empty.
	isEmptyPath := func(path string) bool {
		return path == "" || path == SlashSeparator || path == `\`
	}

	if isEmptyPath(arg) {
		return ep, fmt.Errorf("empty or root endpoint is not supported")
	}

	var isLocal bool
	var host string
	u, err := url.Parse(arg)
	if err == nil && u.Host != "" {
		// URL style of endpoint.
		// Valid URL style endpoint is
		// - Scheme field must contain "http" or "https"
		// - All field should be empty except Host and Path.
		if !((u.Scheme == "http" || u.Scheme == "https") &&
			u.User == nil && u.Opaque == "" && !u.ForceQuery && u.RawQuery == "" && u.Fragment == "") {
			return ep, fmt.Errorf("invalid URL endpoint format")
		}

		var port string
		host, port, err = net.SplitHostPort(u.Host)
		if err != nil {
			if !strings.Contains(err.Error(), "missing port in address") {
				return ep, fmt.Errorf("invalid URL endpoint format: %w", err)
			}

			host = u.Host
		} else {
			var p int
			p, err = strconv.Atoi(port)
			if err != nil {
				return ep, fmt.Errorf("invalid URL endpoint format: invalid port number")
			} else if p < 1 || p > 65535 {
				return ep, fmt.Errorf("invalid URL endpoint format: port number must be between 1 to 65535")
			}
		}
		if i := strings.Index(host, "%"); i > -1 {
			host = host[:i]
		}

		if host == "" {
			return ep, fmt.Errorf("invalid URL endpoint format: empty host name")
		}

		// As this is path in the URL, we should use path package, not filepath package.
		// On MS Windows, filepath.Clean() converts into Windows path style ie `/foo` becomes `\foo`
		u.Path = path.Clean(u.Path)

		// On windows having a preceding SlashSeparator will cause problems, if the
		// command line already has C:/<export-folder/ in it. Final resulting
		// path on windows might become C:/C:/ this will cause problems
		// of starting minio server properly in distributed mode on windows.
		// As a special case make sure to trim the separator.

		// NOTE: It is also perfectly fine for windows users to have a path
		// without C:/ since at that point we treat it as relative path
		// and obtain the full filesystem path as well. Providing C:/
		// style is necessary to provide paths other than C:/,
		// such as F:/, D:/ etc.
		//
		// Another additional benefit here is that this style also
		// supports providing \\host\share support as well.
		if runtime.GOOS == globalWindowsOSName {
			if filepath.VolumeName(u.Path[1:]) != "" {
				u.Path = u.Path[1:]
			}
		}

	} else {
		// Only check if the arg is an ip address and ask for scheme since its absent.
		// localhost, example.com, any FQDN cannot be disambiguated from a regular file path such as
		// /mnt/export1. So we go ahead and start the minio server in FS modes in these cases.
		if isHostIP(arg) {
			return ep, fmt.Errorf("invalid URL endpoint format: missing scheme http or https")
		}
		u = &url.URL{Path: path.Clean(arg)}
		isLocal = true
	}

	return Endpoint{
		URL:     u,
		IsLocal: isLocal,
	}, nil
}

// ZoneEndpoints represent endpoints in a given zone
// along with its setCount and drivesPerSet.
type ZoneEndpoints struct {
	SetCount     int
	DrivesPerSet int
	Endpoints    Endpoints
}

// EndpointZones - list of list of endpoints
type EndpointZones []ZoneEndpoints

// FirstLocal returns true if the first endpoint is local.
func (l EndpointZones) FirstLocal() bool {
	return l[0].Endpoints[0].IsLocal
}

// HTTPS - returns true if secure for URLEndpointType.
func (l EndpointZones) HTTPS() bool {
	return l[0].Endpoints.HTTPS()
}

// Nodes - returns all nodes count
func (l EndpointZones) Nodes() (count int) {
	for _, ep := range l {
		count += len(ep.Endpoints)
	}
	return count
}

// Endpoints - list of same type of endpoint.
type Endpoints []Endpoint

// HTTPS - returns true if secure for URLEndpointType.
func (endpoints Endpoints) HTTPS() bool {
	return endpoints[0].HTTPS()
}

// GetString - returns endpoint string of i-th endpoint (0-based),
// and empty string for invalid indexes.
func (endpoints Endpoints) GetString(i int) string {
	if i < 0 || i >= len(endpoints) {
		return ""
	}
	return endpoints[i].String()
}

// UpdateIsLocal - resolves the host and discovers the local host.
func (endpoints Endpoints) UpdateIsLocal() error {
	var epsResolved int
	var foundLocal bool
	resolvedList := make([]bool, len(endpoints))
	// Mark the starting time
	keepAliveTicker := time.NewTicker(retryInterval * time.Second)
	defer keepAliveTicker.Stop()
	for {
		// Break if the local endpoint is found already Or all the endpoints are resolved.
		if foundLocal || (epsResolved == len(endpoints)) {
			break
		}
		// Retry infinitely on Kubernetes and Docker swarm.
		// This is needed as the remote hosts are sometime
		// not available immediately.
		select {
		case <-globalOSSignalCh:
			return fmt.Errorf("The endpoint resolution got interrupted")
		default:
			for i, resolved := range resolvedList {
				if resolved {
					// Continue if host is already resolved.
					continue
				}

				// return err if not Docker or Kubernetes
				// We use IsDocker() to check for Docker environment
				// We use IsKubernetes() to check for Kubernetes environment
				isLocal, err := isLocalHost(endpoints[i].Hostname(), endpoints[i].Port(), globalRadioPort)
				if err != nil {
					return err
				}
				resolvedList[i] = true
				endpoints[i].IsLocal = isLocal
				epsResolved++
				if !foundLocal {
					foundLocal = isLocal
				}
			}

			if !foundLocal {
				<-keepAliveTicker.C
			}
		}
	}

	// On Kubernetes/Docker setups DNS resolves inappropriately sometimes
	// where there are situations same endpoints with multiple disks
	// come online indicating either one of them is local and some
	// of them are not local. This situation can never happen and
	// its only a possibility in orchestrated deployments with dynamic
	// DNS. Following code ensures that we treat if one of the endpoint
	// says its local for a given host - it is true for all endpoints
	// for the same host. Following code ensures that this assumption
	// is true and it works in all scenarios and it is safe to assume
	// for a given host.
	endpointLocalMap := make(map[string]bool)
	for _, ep := range endpoints {
		if ep.IsLocal {
			endpointLocalMap[ep.Host] = ep.IsLocal
		}
	}
	for i := range endpoints {
		endpoints[i].IsLocal = endpointLocalMap[endpoints[i].Host]
	}
	return nil
}

// NewEndpoints - returns new endpoint list based on input args.
func NewEndpoints(args ...string) (endpoints Endpoints, err error) {
	var endpointType EndpointType
	var scheme string

	uniqueArgs := set.NewStringSet()
	// Loop through args and adds to endpoint list.
	for i, arg := range args {
		endpoint, err := NewEndpoint(arg)
		if err != nil {
			return nil, fmt.Errorf("'%s': %s", arg, err.Error())
		}

		// All endpoints have to be same type and scheme if applicable.
		if i == 0 {
			endpointType = endpoint.Type()
			scheme = endpoint.Scheme
		} else if endpoint.Type() != endpointType {
			return nil, fmt.Errorf("mixed style endpoints are not supported")
		} else if endpoint.Scheme != scheme {
			return nil, fmt.Errorf("mixed scheme is not supported")
		}

		arg = endpoint.String()
		if uniqueArgs.Contains(arg) {
			return nil, fmt.Errorf("duplicate endpoints found")
		}
		uniqueArgs.Add(arg)
		endpoints = append(endpoints, endpoint)
	}

	return endpoints, nil
}

// createEndpoints - validates and creates new endpoints for given args.
func createEndpoints(serverAddr string, args ...string) (Endpoints, error) {
	var endpoints Endpoints
	var err error

	// Check whether serverAddr is valid for this host.
	if err = CheckLocalServerAddr(serverAddr); err != nil {
		return endpoints, err
	}

	_, serverAddrPort := mustSplitHostPort(serverAddr)
	// Convert args to endpoints
	endpoints, err = NewEndpoints(args...)
	if err != nil {
		return endpoints, config.ErrInvalidRadioEndpoints(nil).Msg(err.Error())
	}
	if len(endpoints) == 0 {
		return endpoints, config.ErrInvalidRadioEndpoints(nil).Msg("invalid number of endpoints")
	}

	if err = endpoints.UpdateIsLocal(); err != nil {
		return endpoints, config.ErrInvalidRadioEndpoints(nil).Msg(err.Error())
	}

	// Here all endpoints are URL style.
	endpointPathSet := set.NewStringSet()
	localEndpointCount := 0
	localServerHostSet := set.NewStringSet()
	localPortSet := set.NewStringSet()

	for _, endpoint := range endpoints {
		endpointPathSet.Add(endpoint.Path)
		if endpoint.IsLocal {
			localServerHostSet.Add(endpoint.Hostname())

			var port string
			_, port, err = net.SplitHostPort(endpoint.Host)
			if err != nil {
				port = serverAddrPort
			}
			localPortSet.Add(port)

			localEndpointCount++
		}
	}

	// Check whether same path is not used in endpoints of a host on different port.
	{
		pathIPMap := make(map[string]set.StringSet)
		for _, endpoint := range endpoints {
			host := endpoint.Hostname()
			hostIPSet, _ := getHostIP(host)
			if IPSet, ok := pathIPMap[endpoint.Path]; ok {
				if !IPSet.Intersection(hostIPSet).IsEmpty() {
					return endpoints, config.ErrInvalidRadioEndpoints(nil).Msg(fmt.Sprintf("path '%s' can not be served by different port on same address", endpoint.Path))
				}
				pathIPMap[endpoint.Path] = IPSet.Union(hostIPSet)
			} else {
				pathIPMap[endpoint.Path] = hostIPSet
			}
		}
	}

	// Check whether same path is used for more than 1 local endpoints.
	{
		localPathSet := set.CreateStringSet()
		for _, endpoint := range endpoints {
			if !endpoint.IsLocal {
				continue
			}
			if localPathSet.Contains(endpoint.Path) {
				return endpoints, config.ErrInvalidRadioEndpoints(nil).Msg(fmt.Sprintf("path '%s' cannot be served by different address on same server", endpoint.Path))
			}
			localPathSet.Add(endpoint.Path)
		}
	}

	// All endpoints are pointing to local host
	if len(endpoints) == localEndpointCount {
		// If all endpoints have same port number, then this is XL setup using URL style endpoints.
		if len(localPortSet) == 1 {
			if len(localServerHostSet) > 1 {
				return endpoints, config.ErrInvalidRadioEndpoints(nil).Msg("all local endpoints should not have different hostnames/ips")
			}
			return endpoints, nil
		}

		// Even though all endpoints are local, but those endpoints use different ports.
		// This means it is DistXL setup.
	}

	// Add missing port in all endpoints.
	for i := range endpoints {
		_, port, err := net.SplitHostPort(endpoints[i].Host)
		if err != nil {
			endpoints[i].Host = net.JoinHostPort(endpoints[i].Host, serverAddrPort)
		} else if endpoints[i].IsLocal && serverAddrPort != port {
			// If endpoint is local, but port is different than serverAddrPort, then make it as remote.
			endpoints[i].IsLocal = false
		}
	}

	uniqueArgs := set.NewStringSet()
	for _, endpoint := range endpoints {
		uniqueArgs.Add(endpoint.Host)
	}

	// Error out if we have less than 2 unique servers.
	if len(uniqueArgs.ToSlice()) < 2 {
		err := fmt.Errorf("Unsupported number of endpoints (%s), minimum number of servers cannot be less than 2 in distributed setup", endpoints)
		return endpoints, err
	}

	return endpoints, nil
}

// Parses all arguments and returns an endpointSet which is a collection
// of endpoints following the ellipses pattern, this is what is used
// by the object layer for initializing itself.
func parseEndpoint(peers string) (endpoints []string, err error) {
	patterns, perr := ellipses.FindEllipsesPatterns(peers)
	if perr != nil {
		return nil, config.ErrInvalidRadioEndpoints(nil).Msg(perr.Error())
	}
	for _, lbls := range patterns.Expand() {
		endpoints = append(endpoints, strings.Join(lbls, ""))
	}
	return endpoints, nil
}

// CreateServerEndpoints - validates and creates new endpoints from input args, supports
// both ellipses and without ellipses transparently.
func createServerEndpoints(serverAddr string, peers string) (Endpoints, error) {
	if len(peers) == 0 {
		return nil, nil
	}

	if !ellipses.HasEllipses(peers) {
		return nil, config.ErrInvalidRadioEndpoints(nil).Msg(fmt.Sprintf("Input args (%s) must have ellipses",
			peers))
	}

	eps, err := parseEndpoint(peers)
	if err != nil {
		return nil, err
	}

	return createEndpoints(serverAddr, eps...)
}
