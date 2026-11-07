package main

import (
	"strings"
	"testing"
)

func TestRefusesToStartWithoutAPassword(t *testing.T) {
	_, _, err := open(t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "BINNY_PASSWORD") {
		t.Fatalf("open without a password returned %v, want an error naming BINNY_PASSWORD", err)
	}
}
