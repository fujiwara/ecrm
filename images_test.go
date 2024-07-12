package ecrm_test

import (
	"testing"

	"github.com/fujiwara/ecrm"
)

func TestImageID(t *testing.T) {
	id := ecrm.ImageID("0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9")
	if id.String() != "0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:fe668fb9" {
		t.Errorf("unexpected image id: %s", id)
	}
	if id.Short() != "foo/bar:fe668fb9" {
		t.Errorf("unexpected short image id: %s", id)
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

	j := make(ecrm.Images)
	j.Add("foo", "qux")
	j.Add("bar", "qux")
	j.Add("baz", "qux")
	images.Merge(j)
	for _, v := range []ecrm.ImageID{"foo", "bar", "baz"} {
		if !images.Contains(v) {
			t.Errorf("unexpected not contains: %s", v)
		}
	}
	t.Logf("images: %#v", images)
}
