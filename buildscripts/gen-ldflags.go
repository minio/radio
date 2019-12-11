// +build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func genLDFlags(version string) string {
	ldflagsStr := "-s -w"
	ldflagsStr += " -X github.com/minio/radio/cmd.Version=" + version
	ldflagsStr += " -X github.com/minio/radio/cmd.ReleaseTag=" + releaseTag(version)
	ldflagsStr += " -X github.com/minio/radio/cmd.CommitID=" + commitID()
	ldflagsStr += " -X github.com/minio/radio/cmd.ShortCommitID=" + commitID()[:12]
	ldflagsStr += " -X github.com/minio/radio/cmd.GOPATH=" + os.Getenv("GOPATH")
	ldflagsStr += " -X github.com/minio/radio/cmd.GOROOT=" + os.Getenv("GOROOT")
	return ldflagsStr
}

// genReleaseTag prints release tag to the console for easy git tagging.
func releaseTag(version string) string {
	relPrefix := "DEVELOPMENT"
	if prefix := os.Getenv("RADIO_RELEASE"); prefix != "" {
		relPrefix = prefix
	}

	relTag := strings.Replace(version, " ", "-", -1)
	relTag = strings.Replace(relTag, ":", "-", -1)
	relTag = strings.Replace(relTag, ",", "", -1)
	return relPrefix + "." + relTag
}

// commitID returns the abbreviated commit-id hash of the last commit.
func commitID() string {
	// git log --format="%h" -n1
	var (
		commit []byte
		e      error
	)
	cmdName := "git"
	cmdArgs := []string{"log", "--format=%H", "-n1"}
	if commit, e = exec.Command(cmdName, cmdArgs...).Output(); e != nil {
		fmt.Fprintln(os.Stderr, "Error generating git commit-id: ", e)
		os.Exit(1)
	}

	return strings.TrimSpace(string(commit))
}

func main() {
	fmt.Println(genLDFlags(time.Now().UTC().Format(time.RFC3339)))
}
