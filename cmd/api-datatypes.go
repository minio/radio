package cmd

import (
	"encoding/xml"
)

// ObjectIdentifier carries key name for the object to delete.
type ObjectIdentifier struct {
	ObjectName string `xml:"Key"`
}

// createBucketConfiguration container for bucket configuration request from client.
// Used for parsing the location from the request body for Makebucket.
type createBucketLocationConfiguration struct {
	XMLName  xml.Name `xml:"CreateBucketConfiguration" json:"-"`
	Location string   `xml:"LocationConstraint"`
}

// DeleteObjectsRequest - xml carrying the object key names which needs to be deleted.
type DeleteObjectsRequest struct {
	// Element to enable quiet mode for the request
	Quiet bool
	// List of objects to be deleted
	Objects []ObjectIdentifier `xml:"Object"`
}
