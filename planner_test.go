package ecrm_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/fujiwara/ecrm"
)

func TestIsKeptImageIndex(t *testing.T) {
	region := "ap-northeast-1"
	registryID := "012345678901"
	repoName := "my-service"

	tests := []struct {
		name       string
		detail     ecrTypes.ImageDetail
		keepImages ecrm.Images
		want       bool
	}{
		{
			name: "kept by digest",
			detail: ecrTypes.ImageDetail{
				RegistryId:     aws.String(registryID),
				RepositoryName: aws.String(repoName),
				ImageDigest:    aws.String("sha256:aaa"),
			},
			keepImages: func() ecrm.Images {
				m := make(ecrm.Images)
				m.Add("012345678901.dkr.ecr.ap-northeast-1.amazonaws.com/my-service@sha256:aaa", "taskdef")
				return m
			}(),
			want: true,
		},
		{
			name: "kept by tag",
			detail: ecrTypes.ImageDetail{
				RegistryId:     aws.String(registryID),
				RepositoryName: aws.String(repoName),
				ImageDigest:    aws.String("sha256:bbb"),
				ImageTags:      []string{"v1.0"},
			},
			keepImages: func() ecrm.Images {
				m := make(ecrm.Images)
				m.Add("012345678901.dkr.ecr.ap-northeast-1.amazonaws.com/my-service:v1.0", "taskdef")
				return m
			}(),
			want: true,
		},
		{
			name: "not kept",
			detail: ecrTypes.ImageDetail{
				RegistryId:     aws.String(registryID),
				RepositoryName: aws.String(repoName),
				ImageDigest:    aws.String("sha256:ccc"),
				ImageTags:      []string{"old"},
			},
			keepImages: func() ecrm.Images {
				m := make(ecrm.Images)
				m.Add("012345678901.dkr.ecr.ap-northeast-1.amazonaws.com/other-service:latest", "taskdef")
				return m
			}(),
			want: false,
		},
		{
			name: "not kept with empty keepImages",
			detail: ecrTypes.ImageDetail{
				RegistryId:     aws.String(registryID),
				RepositoryName: aws.String(repoName),
				ImageDigest:    aws.String("sha256:ddd"),
			},
			keepImages: make(ecrm.Images),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ecrm.IsKeptImageIndex(tt.detail, region, tt.keepImages)
			if got != tt.want {
				t.Errorf("IsKeptImageIndex() = %v, want %v", got, tt.want)
			}
		})
	}
}
