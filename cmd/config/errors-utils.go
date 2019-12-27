package config

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/minio/minio/pkg/color"
	"gopkg.in/yaml.v2"
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

// Clone returns a new Err struct with the same information
func (u *Err) Clone() *Err {
	return &Err{
		msg:    u.msg,
		detail: u.detail,
		action: u.action,
		hint:   u.hint,
	}
}

// Return the error message
func (u *Err) Error() string {
	if u.detail == "" {
		if u.msg != "" {
			return u.msg
		}
		return "<nil>"
	}
	return u.detail
}

// Msg - Replace the current error's message
func (u *Err) Msg(m string, args ...interface{}) *Err {
	e := u.Clone()
	e.msg = fmt.Sprintf(m, args...)
	return e
}

// Hint - Replace the current error's message
func (u *Err) Hint(m string, args ...interface{}) *Err {
	e := u.Clone()
	e.hint = fmt.Sprintf(m, args...)
	return e
}

// ErrFn function wrapper
type ErrFn func(err error) *Err

// Create a UI error generator, this is needed to simplify
// the update of the detailed error message in several places
// in MinIO code
func newErrFn(msg, action, hint string) ErrFn {
	return func(err error) *Err {
		u := &Err{
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
func ErrorToErr(err error) error {
	if err == nil {
		return nil
	}

	// If this is already a Err, do nothing
	if e, ok := err.(*Err); ok {
		return e
	}

	var ymlErr *yaml.TypeError

	// Show a generic message for known golang errors
	if errors.Is(err, syscall.EADDRINUSE) {
		return ErrPortAlreadyInUse(err).Msg("specified port is already in use")
	} else if errors.Is(err, syscall.EACCES) {
		return ErrPortAccess(err).Msg("insufficient permissions to use specified port")
	} else if os.IsNotExist(err) {
		return ErrConfigInvalid(err).Msg("config file not found")
	} else if os.IsPermission(err) {
		return ErrConfigInvalid(err).Msg("insufficient permissions to read from config file")
	} else if errors.As(err, &ymlErr) {
		return ErrConfigInvalid(err).Msg("config is not parsable")
	} else {
		// Failed to identify what type of error this, return a simple UI error
		return &Err{msg: err.Error()}
	}
}

// FmtError converts a fatal error message to a more clear error
// using some colors
func FmtError(introMsg string, err error, jsonFlag bool) string {
	renderedTxt := ""
	eerr := ErrorToErr(err)

	var uiErr *Err
	if !errors.As(eerr, &uiErr) {
		panic("ErrorToErr cannot return err without type *config.Err")
	}

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
