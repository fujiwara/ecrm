package ecrm_test

import (
	"testing"

	"github.com/fujiwara/ecrm"
)

func TestParseTaskDefArn(t *testing.T) {
	arn := "arn:aws:ecs:ap-northeast-1:0123456789012:task-definition/ecspresso-test:884"
	td, err := ecrm.ParseTaskdefArn(arn)
	if err != nil {
		t.Fatal(err)
	}
	if td.String() != "ecspresso-test:884" {
		t.Errorf("unexpected task definition: %s", td)
	}
}
