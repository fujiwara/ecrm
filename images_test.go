package ecrm_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/fujiwara/ecrm"
	"github.com/google/go-cmp/cmp"
)

func TestImageURI(t *testing.T) {
	u := ecrm.ImageURI("0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9")
	if u.String() != "0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9" {
		t.Errorf("unexpected image uri: %s", u)
	}
	if u.Short() != "foo/bar:fe668fb9" {
		t.Errorf("unexpected short image uri: %s", u)
	}
	if u.Base() != "0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar" {
		t.Errorf("unexpected base image uri: %s", u)
	}
	if u.Tag() != "fe668fb9" {
		t.Errorf("unexpected tag image uri: %s", u)
	}
	if u.IsDigestURI() {
		t.Errorf("unexpected digest uri: %s", u)
	}
}

func TestImageURIDigest(t *testing.T) {
	u := ecrm.ImageURI("0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar@sha256:b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c")
	if u.String() != "0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar@sha256:b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c" {
		t.Errorf("unexpected image uri: %s", u)
	}
	if u.Short() != "foo/bar@sha256:b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c" {
		t.Errorf("unexpected short image uri: %s", u)
	}
	if u.Base() != "0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar" {
		t.Errorf("unexpected base image uri: %s", u)
	}
	if u.Tag() != "" {
		t.Errorf("unexpected tag image uri: %s", u)
	}
	if !u.IsDigestURI() {
		t.Errorf("unexpected not digest uri: %s", u)
	}
}

func TestImages(t *testing.T) {
	images := make(ecrm.Images)

	if images.Contains("foo") {
		t.Error("unexpected contains")
	}
	images.Add("foo", "bar")
	if !images.Contains("foo") {
		t.Error("unexpected not contains")
	}
	if images.Contains("bar") {
		t.Error("unexpected contains")
	}
	images.Add("foo", "baz")
	if !images.Contains("foo") {
		t.Error("unexpected not contains")
	}
	if images.Add("foo", "baz") {
		// already exists
		t.Error("unexpected added")
	}

	j := make(ecrm.Images)
	j.Add("foo", "qux")
	j.Add("bar", "qux")
	j.Add("baz", "qux")
	images.Merge(j)
	for _, v := range []ecrm.ImageURI{"foo", "bar", "baz"} {
		if !images.Contains(v) {
			t.Errorf("unexpected not contains: %s", v)
		}
	}
	t.Logf("images: %#v", images)
}

func TestLoadImages(t *testing.T) {
	images := make(ecrm.Images)
	images.Add("9876543210987.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9", "baz")
	images.Add("0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9", "baz")
	err := images.LoadFile("testdata/images.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 4 {
		t.Errorf("unexpected images: %d", len(images))
	}
	t.Logf("images: %#v", images)
}

func TestPrintImages(t *testing.T) {
	images := make(ecrm.Images)
	images.Add("9876543210987.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9", "baz")
	images.Add("0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9", "baz")
	b := &bytes.Buffer{}
	err := images.Print(b)
	if err != nil {
		t.Fatal(err)
	}
	restored := []string{}
	if err := json.NewDecoder(b).Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{
		"0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9",
		"9876543210987.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9",
	}, restored); diff != "" {
		t.Errorf("unexpected images: %s", diff)
	}
}
