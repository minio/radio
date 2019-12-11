package config

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/minio/minio/pkg/color"
)

// Err is a structure which contains all information
// to print a fatal error message in json or pretty mode
// Err implements error so we can use it anywhere
type Err struct {
	msg    string
	detail string
	action string
	hint   string
}

// Return the error message
func (u Err) Error() string {
	if u.detail == "" {
		return u.msg
	}
	return u.detail
}

// Msg - Replace the current error's message
func (u Err) Msg(m string, args ...interface{}) Err {
	return Err{
		msg:    fmt.Sprintf(m, args...),
		detail: u.detail,
		action: u.action,
		hint:   u.hint,
	}
}

// ErrFn function wrapper
type ErrFn func(err error) Err

// Create a UI error generator, this is needed to simplify
// the update of the detailed error message in several places
// in MinIO code
func newErrFn(msg, action, hint string) ErrFn {
	return func(err error) Err {
		u := Err{
			msg:    msg,
			action: action,
			hint:   hint,
		}
		if err != nil {
			u.detail = err.Error()
		}
		return u
	}
}

// ErrorToErr inspects the passed error and transforms it
// to the appropriate UI error.
func ErrorToErr(err error) Err {
	// If this is already a Err, do nothing
	if e, ok := err.(Err); ok {
		return e
	}

	// Show a generic message for known golang errors
	if errors.Is(err, syscall.EADDRINUSE) {
		return ErrPortAlreadyInUse(err).Msg("Specified port is already in use")
	} else if errors.Is(err, syscall.EACCES) {
		return ErrPortAccess(err).Msg("Insufficient permissions to use specified port")
	} else {
		// Failed to identify what type of error this, return a simple UI error
		return Err{msg: err.Error()}
	}

}

// FmtError converts a fatal error message to a more clear error
// using some colors
func FmtError(introMsg string, err error, jsonFlag bool) string {
	renderedTxt := ""
	uiErr := ErrorToErr(err)
	// JSON print
	if jsonFlag {
		// Message text in json should be simple
		if uiErr.detail != "" {
			return uiErr.msg + ": " + uiErr.detail
		}
		return uiErr.msg
	}
	// Pretty print error message
	introMsg += ": "
	if uiErr.msg != "" {
		introMsg += color.Bold(uiErr.msg)
	} else {
		introMsg += color.Bold(err.Error())
	}
	renderedTxt += color.Red(introMsg) + "\n"
	// Add action message
	if uiErr.action != "" {
		renderedTxt += "> " + color.BgYellow(color.Black(uiErr.action)) + "\n"
	}
	// Add hint
	if uiErr.hint != "" {
		renderedTxt += color.Bold("HINT:") + "\n"
		renderedTxt += "  " + uiErr.hint
	}
	return renderedTxt
}
