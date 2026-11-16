package main

import (
	"bytes"
	"testing"
)

func TestAccessWriteNeedsApprovalBeforeReadingEnvironment(t *testing.T) {
	for _, args := range [][]string{{"access", "init", "--env", "missing.env"}, {"access", "status", "--allow-write", "--env", "missing.env"}, {"access", "delete"}} {
		var out, diagnostics bytes.Buffer
		if run(args, &out, &diagnostics) != 2 {
			t.Fatal("unsafe access CLI arguments accepted")
		}
		if bytes.Contains(diagnostics.Bytes(), []byte("cannot read environment")) {
			t.Fatal("write approval checked too late")
		}
	}
}
