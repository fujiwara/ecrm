package ecrm_test

import (
	"context"
	"testing"

	"github.com/fujiwara/ecrm"
)

func TestReadExcludeFile(t *testing.T) {
	images := ecrm.NewImagesSet()
	err := ecrm.ReadExcludeFile(context.Background(), "testdata/my_exclude_file.txt", images)
	if err != nil {
		t.Error("failed to ReadExcludeFile", err)
	}
	if len(images) != 2 {
		t.Error("failed to ReadExcludeFile")
	}
	for _, img := range []string{
		"0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/xxx/yyy:foo",
		"0123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/xxx/yyy:bar",
	} {
		if _, ok := images[img]; !ok {
			t.Error("img not found", img)
		}
	}
}
