package logger

import (
	"fmt"
	"regexp"

	"github.com/minio/minio/pkg/color"
)

var ansiRE = regexp.MustCompile("(\x1b[^m]*m)")

// Print ANSI Control escape
func ansiEscape(format string, args ...interface{}) {
	var Esc = "\x1b"
	fmt.Printf("%s%s", Esc, fmt.Sprintf(format, args...))
}

func ansiMoveRight(n int) {
	if color.IsTerminal() {
		ansiEscape("[%dC", n)
	}
}

func ansiSaveAttributes() {
	if color.IsTerminal() {
		ansiEscape("7")
	}
}

func ansiRestoreAttributes() {
	if color.IsTerminal() {
		ansiEscape("8")
	}

}
