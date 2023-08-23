package ecrm

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	ociTypes "github.com/google/go-containerregistry/pkg/v1/types"
)

func isContainerImage(d ecrTypes.ImageDetail) bool {
	t := ociTypes.MediaType(aws.ToString(d.ArtifactMediaType))
	return t == ociTypes.DockerConfigJSON || t == ociTypes.OCIConfigJSON
}

func isContainerImageFilter(d ecrTypes.ImageDetail, _ int) bool {
	return isContainerImage(d)
}

func isImageIndex(d ecrTypes.ImageDetail) bool {
	if aws.ToString(d.ArtifactMediaType) != "" {
		return false
	}
	switch ociTypes.MediaType(aws.ToString(d.ImageManifestMediaType)) {
	case ociTypes.OCIImageIndex:
		return true
	}
	return false
}

func isImageIndexFilter(d ecrTypes.ImageDetail, _ int) bool {
	return isImageIndex(d)
}

func isSociIndex(d ecrTypes.ImageDetail) bool {
	return ociTypes.MediaType(aws.ToString(d.ArtifactMediaType)) == MediaTypeSociIndex
}

func isSociIndexFilter(d ecrTypes.ImageDetail, _ int) bool {
	return isSociIndex(d)
}
