package ecrm

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
)

type taskdef struct {
	name     string
	revision int
}

func (td taskdef) String() string {
	return fmt.Sprintf("%s:%d", td.name, td.revision)
}

func parseTaskdefArn(s string) (taskdef, error) {
	a, err := arn.Parse(s)
	if err != nil {
		return taskdef{}, err
	}
	if a.Service != "ecs" {
		return taskdef{}, errors.New("not an ECS task definition ARN")
	}
	p := strings.SplitN(a.Resource, "/", 2)
	if len(p) != 2 {
		return taskdef{}, errors.New("invalid task definition ARN")
	}
	if p[0] != "task-definition" {
		return taskdef{}, errors.New("not a task definition ARN")
	}
	nr := strings.SplitN(p[1], ":", 2)
	if len(nr) != 2 {
		return taskdef{}, errors.New("invalid task definition name")
	}
	rev, err := strconv.Atoi(nr[1])
	if err != nil {
		return taskdef{}, err
	}
	return taskdef{name: nr[0], revision: rev}, nil
}
