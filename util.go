package ecrm

import (
	"context"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdaTypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	ociTypes "github.com/google/go-containerregistry/pkg/v1/types"
)

func isContainerImage(d ecrTypes.ImageDetail) bool {
	t := ociTypes.MediaType(aws.ToString(d.ArtifactMediaType))
	return t == ociTypes.DockerConfigJSON || t == ociTypes.OCIConfigJSON
}

func isImageIndex(d ecrTypes.ImageDetail) bool {
	if aws.ToString(d.ArtifactMediaType) != "" {
		return false
	}
	switch ociTypes.MediaType(aws.ToString(d.ImageManifestMediaType)) {
	case ociTypes.OCIImageIndex:
		return true
	case ociTypes.DockerManifestList:
		return true
	}
	return false
}

func isSociIndex(d ecrTypes.ImageDetail) bool {
	return ociTypes.MediaType(aws.ToString(d.ArtifactMediaType)) == MediaTypeSociIndex
}

func taskDefinitionFamilies(ctx context.Context, client *ecs.Client) ([]string, error) {
	tds := make([]string, 0)
	p := ecs.NewListTaskDefinitionFamiliesPaginator(client, &ecs.ListTaskDefinitionFamiliesInput{})
	for p.HasMorePages() {
		td, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		log.Println("[debug] task definition families:", td.Families)
		tds = append(tds, td.Families...)
	}
	return tds, nil
}

func clusterArns(ctx context.Context, client *ecs.Client) ([]string, error) {
	clusters := make([]string, 0)
	p := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for p.HasMorePages() {
		co, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, co.ClusterArns...)
	}
	return clusters, nil
}

func lambdaFunctions(ctx context.Context, client *lambda.Client) ([]lambdaTypes.FunctionConfiguration, error) {
	fns := make([]lambdaTypes.FunctionConfiguration, 0)
	p := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for p.HasMorePages() {
		r, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, fn := range r.Functions {
			if fn.PackageType != "Image" {
				continue
			}
			log.Printf("[debug] lambda function %s PackageType %s", *fn.FunctionName, fn.PackageType)
			fns = append(fns, fn)
		}
	}
	return fns, nil
}

func ecrRepositories(ctx context.Context, client *ecr.Client) ([]ecrTypes.Repository, error) {
	repos := make([]ecrTypes.Repository, 0)
	p := ecr.NewDescribeRepositoriesPaginator(client, &ecr.DescribeRepositoriesInput{})
	for p.HasMorePages() {
		repo, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo.Repositories...)
	}
	return repos, nil
}

func arnToName(name, removePrefix string) string {
	if arn.IsARN(name) {
		a, _ := arn.Parse(name)
		return strings.Replace(a.Resource, removePrefix, "", 1)
	}
	return name
}

func clusterArnToName(arn string) string {
	return arnToName(arn, "cluster/")
}
