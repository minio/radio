package cmd

// ObjectIdentifier carries key name for the object to delete.
type ObjectIdentifier struct {
	ObjectName string `xml:"Key"`
}

// DeleteObjectsRequest - xml carrying the object key names which needs to be deleted.
type DeleteObjectsRequest struct {
	// Element to enable quiet mode for the request
	Quiet bool
	// List of objects to be deleted
	Objects []ObjectIdentifier `xml:"Object"`
}
