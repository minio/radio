package cmd

import "net/http"

// Standard cross domain policy information located at https://s3.amazonaws.com/crossdomain.xml
const crossDomainXML = `<?xml version="1.0"?><!DOCTYPE cross-domain-policy SYSTEM "http://www.adobe.com/xml/dtds/cross-domain-policy.dtd"><cross-domain-policy><allow-access-from domain="*" secure="false" /></cross-domain-policy>`

// Standard path where an app would find cross domain policy information.
const crossDomainXMLEntity = "/crossdomain.xml"

// Cross domain policy implements http.Handler interface, implementing a custom ServerHTTP.
type crossDomainPolicy struct {
	handler http.Handler
}

// A cross-domain policy file is an XML document that grants a web client, such as Adobe Flash Player
// or Adobe Acrobat (though not necessarily limited to these), permission to handle data across domains.
// When clients request content hosted on a particular source domain and that content make requests
// directed towards a domain other than its own, the remote domain needs to host a cross-domain
// policy file that grants access to the source domain, allowing the client to continue the transaction.
func setCrossDomainPolicy(h http.Handler) http.Handler {
	return crossDomainPolicy{handler: h}
}

func (c crossDomainPolicy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Look for 'crossdomain.xml' in the incoming request.
	switch r.URL.Path {
	case crossDomainXMLEntity:
		// Write the standard cross domain policy xml.
		w.Write([]byte(crossDomainXML))
		// Request completed, no need to serve to other handlers.
		return
	}
	// Continue to serve the request further.
	c.handler.ServeHTTP(w, r)
}
