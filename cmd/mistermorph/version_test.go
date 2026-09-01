package main

import (
	"bytes"
	"testing"
)

func TestVersionCommandUsesExecutableName(t *testing.T) {
	cmd := newVersionCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := out.String(), "morph dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}
