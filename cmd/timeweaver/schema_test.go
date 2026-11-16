package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSchemaRepairRequiresOptIn(t *testing.T) {
	var out, diag bytes.Buffer
	if code := run([]string{"schema", "repair", "--env", "missing-file"}, &out, &diag); code != 2 || !strings.Contains(diag.String(), "--allow-write") {
		t.Fatal(code, &diag)
	}
	for _, args := range [][]string{{"schema"}, {"schema", "up"}, {"schema", "status", "--allow-write"}} {
		if code := run(args, &out, &diag); code != 2 {
			t.Fatal(args, code)
		}
	}
}
