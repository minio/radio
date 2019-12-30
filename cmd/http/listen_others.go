// +build windows plan9 solaris

package http

import "net"

// Windows, plan9 specific listener.
var listen = net.Listen
var fallbackListen = net.Listen
