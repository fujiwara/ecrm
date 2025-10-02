package ecrm_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/fujiwara/ecrm"
	"github.com/google/go-cmp/cmp"
)

func TestExternalCommand(t *testing.T) {
	ext := &ecrm.ExternalCommand{
		Command: []string{"sh", "-c", `printf '["foobar:%s"]' "${TAG}"`},
		Env:     map[string]string{"TAG": "deadbeef"},
	}
	b, err := ext.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Diff(string(b), `["foobar:deadbeef"]`) != "" {
		t.Errorf("unexpected output: %s", b)
	}
}

func TestExternalCommandDir(t *testing.T) {
	ext := &ecrm.ExternalCommand{
		Command: []string{"sh", "-c", "pwd"},
		Dir:     "/",
	}
	b, err := ext.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Diff(string(bytes.TrimSpace(b)), "/") != "" {
		t.Errorf("unexpected output: %s", b)
	}
}

func TestExternalCommandTimeout(t *testing.T) {
	ext := &ecrm.ExternalCommand{
		Command: []string{"sh", "-c", "sleep 0.3"},
		Timeout: time.Millisecond * 100,
	}
	_, err := ext.Run(t.Context())
	if err == nil {
		t.Fatal("should be errored by timeout")
	}
	t.Log(err)
}
