package logger

// Target is the entity that we will receive
// a single log entry and Send it to the log target
//   e.g. Send the log to a http server
type Target interface {
	Send(entry interface{}, errKind string) error
}

// Targets is the set of enabled loggers
var Targets = []Target{}

// AddTarget adds a new logger target to the
// list of enabled loggers
func AddTarget(t Target) {
	Targets = append(Targets, t)
}
