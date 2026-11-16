// Compiled only inside the pinned TimeWeaver-Agent checkout in disposable CI.
// It uses the real protocol decoder; it does not execute tasks or enroll devices.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/horrible-gh/TimeWeaver-Agent/internal/protocol"
)

func main() {
	var input struct {
		Envelope protocol.Envelope `json:"envelope"`
		ETag     string            `json:"etag"`
		Identity protocol.Identity `json:"identity"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Fprintln(os.Stderr, "invalid test input")
		os.Exit(1)
	}
	if _, err := protocol.DecodeSnapshot(input.Envelope, input.ETag, input.Identity); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
