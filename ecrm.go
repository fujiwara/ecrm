package ecrm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Songmu/prompter"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecsTypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/samber/lo"

	oci "github.com/google/go-containerregistry/pkg/v1"
	ociTypes "github.com/google/go-containerregistry/pkg/v1/types"
)

var untaggedStr = "__UNTAGGED__"

type App struct {
	ecr    *ecr.Client
	ecs    *ecs.Client
	lambda *lambda.Client
	region string
}

type taskdef struct {
	name     string
	revision int
}

func (td taskdef) String() string {
	return fmt.Sprintf("%s:%d", td.name, td.revision)
}

type Option struct {
	Delete     bool
	Force      bool
	Repository string
	NoColor    bool
	Format     outputFormat
}

func New(ctx context.Context, region string) (*App, error) {
	cfg, err := awsConfig.LoadDefaultConfig(ctx, awsConfig.WithRegion(region))
	if err != nil {
		return nil, err
	}

	return &App{
		region: cfg.Region,
		ecr:    ecr.NewFromConfig(cfg),
		ecs:    ecs.NewFromConfig(cfg),
		lambda: lambda.NewFromConfig(cfg),
	}, nil
}

func (app *App) clusterArns(ctx context.Context) ([]string, error) {
	clusters := make([]string, 0)
	p := ecs.NewListClustersPaginator(app.ecs, &ecs.ListClustersInput{})
	for p.HasMorePages() {
		co, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, co.ClusterArns...)
	}
	return clusters, nil
}

func (app *App) taskDefinitionFamilies(ctx context.Context) ([]string, error) {
	tds := make([]string, 0)
	p := ecs.NewListTaskDefinitionFamiliesPaginator(app.ecs, &ecs.ListTaskDefinitionFamiliesInput{})
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

func (app *App) Run(ctx context.Context, path string, opt Option) error {
	c, err := LoadConfig(path)
	if err != nil {
		return err
	}

	var taskdefs []taskdef
	if tds, err := app.scanClusters(ctx, c.Clusters); err != nil {
		return err
	} else {
		taskdefs = append(taskdefs, tds...)
	}
	if tds, err := app.scanTaskdefs(ctx, c.TaskDefinitions); err != nil {
		return err
	} else {
		taskdefs = append(taskdefs, tds...)
	}
	images, err := app.aggregateECRImages(ctx, taskdefs)
	if err != nil {
		return err
	}

	if err := app.scanLambdaFunctions(ctx, c.LambdaFunctions, images); err != nil {
		return err
	}

	idsMaps, err := app.scanRepositories(ctx, c.Repositories, images, opt)
	if err != nil {
		return err
	}
	if !opt.Delete {
		return nil
	}
	for name, ids := range idsMaps {
		if err := app.DeleteImages(ctx, name, ids, opt); err != nil {
			return err
		}
	}

	return nil
}

func (app *App) aggregateECRImages(ctx context.Context, taskdefs []taskdef) (map[string]set, error) {
	images := make(map[string]set)
	dup := make(map[string]struct{})
	for _, td := range taskdefs {
		if _, found := dup[td.String()]; found {
			continue
		}
		dup[td.String()] = struct{}{}

		imgs, err := app.extractECRImages(ctx, td.String())
		if err != nil {
			return nil, err
		}
		for _, img := range imgs {
			log.Printf("[info] %s is in use by task definition %s", img, td.String())
			if images[img] == nil {
				images[img] = newSet()
			}
			images[img].add(td.String())
		}
	}
	return images, nil
}

func (app *App) repositories(ctx context.Context) ([]ecrTypes.Repository, error) {
	repos := make([]ecrTypes.Repository, 0)
	p := ecr.NewDescribeRepositoriesPaginator(app.ecr, &ecr.DescribeRepositoriesInput{})
	for p.HasMorePages() {
		repo, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo.Repositories...)
	}
	return repos, nil
}

func (app *App) scanRepositories(ctx context.Context, rcs []*RepositoryConfig, images map[string]set, opt Option) (map[string][]ecrTypes.ImageIdentifier, error) {
	idsMaps := make(map[string][]ecrTypes.ImageIdentifier)
	var sums summaries
	in := &ecr.DescribeRepositoriesInput{}
	if opt.Repository != "" {
		in.RepositoryNames = []string{opt.Repository}
	}
	p := ecr.NewDescribeRepositoriesPaginator(app.ecr, in)
	for p.HasMorePages() {
		repos, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
	REPO:
		for _, repo := range repos.Repositories {
			name := *repo.RepositoryName
			var rc *RepositoryConfig
			for _, _rc := range rcs {
				if _rc.MatchName(name) {
					rc = _rc
					break
				}
			}
			if rc == nil {
				continue REPO
			}
			ids, sum, err := app.unusedImageIdentifiers(ctx, name, rc, images)
			if err != nil {
				return nil, err
			}
			sums = append(sums, sum)
			idsMaps[name] = ids
		}
	}
	sort.SliceStable(sums, func(i, j int) bool {
		return sums[i].Repo < sums[j].Repo
	})
	if err := sums.print(os.Stdout, opt.NoColor, opt.Format); err != nil {
		return nil, err
	}
	return idsMaps, nil
}

const batchDeleteImageIdsLimit = 100
const batchGetImageLimit = 100

func (app *App) DeleteImages(ctx context.Context, repo string, ids []ecrTypes.ImageIdentifier, opt Option) error {
	if len(ids) == 0 {
		log.Println("[info] no need to delete images on", repo)
		return nil
	}
	if !opt.Delete {
		log.Printf("[notice] Expired %d image(s) found on %s. Run delete command to delete them.", len(ids), repo)
		return nil
	}
	if !opt.Force {
		if !prompter.YN(fmt.Sprintf("Do you delete %d images on %s?", len(ids), repo), false) {
			return errors.New("aborted")
		}
	}

	for _, id := range ids {
		log.Printf("[notice] Deleting %s %s", repo, *id.ImageDigest)
	}
	chunkIDs := lo.Chunk(ids, batchDeleteImageIdsLimit)
	var deletedCount int
	defer func() {
		log.Printf("[info] Deleted %d images on %s", deletedCount, repo)
	}()
	for _, ids := range chunkIDs {
		output, err := app.ecr.BatchDeleteImage(ctx, &ecr.BatchDeleteImageInput{
			ImageIds:       ids,
			RepositoryName: &repo,
		})
		if err != nil {
			return err
		}
		deletedCount += len(output.ImageIds)
	}
	return nil
}

func (app *App) unusedImageIdentifiers(ctx context.Context, repo string, rc *RepositoryConfig, holdImages map[string]set) ([]ecrTypes.ImageIdentifier, *summary, error) {
	sum := &summary{
		Repo:             repo,
		TotalImages:      0,
		ExpiredImages:    0,
		TotalImageSize:   0,
		ExpiredImageSize: 0,
	}
	details, foundTags, err := app.listImageDetails(ctx, repo)
	if err != nil {
		return nil, sum, err
	}
	expiredIds := make([]ecrTypes.ImageIdentifier, 0)
	expiredManifests := make(map[string]struct{})
	var keepCount int64
IMAGE:
	for _, d := range details {
		hold := false
		sum.TotalImages++
		sum.TotalImageSize += aws.ToInt64(d.ImageSizeInBytes)
	TAG:
		for _, tag := range d.ImageTags {
			if rc.MatchTag(tag) {
				hold = true
				break TAG
			}
			imageArn := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", *d.RegistryId, app.region, *d.RepositoryName, tag)
			if holdImages[imageArn] != nil && !holdImages[imageArn].isEmpty() {
				hold = true
				break TAG
			}
		}
		if hold {
			continue IMAGE
		}
		pushedAt := *d.ImagePushedAt
		tag, tagged := imageTag(d)
		displayName := repo + ":" + tag
		if !rc.IsExpired(pushedAt) {
			log.Println("[info]", displayName, "is not expired")
			continue IMAGE
		}
		if tagged {
			keepCount++
			if keepCount <= rc.KeepCount {
				log.Printf("[info] %s is in keep_count %d <= %d", displayName, keepCount, rc.KeepCount)
				continue IMAGE
			}
		}
		log.Printf("[notice] %s is expired %s %s", displayName, *d.ImageDigest, pushedAt.Format(time.RFC3339))
		expiredIds = append(expiredIds, ecrTypes.ImageIdentifier{ImageDigest: d.ImageDigest})

		// expired manifest
		tagSha256 := strings.Replace(*d.ImageDigest, "sha256:", "sha256-", 1)
		if _, found := foundTags[tagSha256]; found {
			// image index that has sha256 digest as tag
			log.Printf("[notice] %s:%s is expired (manifest) %s", repo, tagSha256, pushedAt.Format(time.RFC3339))
			expiredIds = append(expiredIds, ecrTypes.ImageIdentifier{ImageTag: aws.String(tagSha256)})
			expiredManifests[tagSha256] = struct{}{}
			sum.expiredManifests++
		}
		sum.ExpiredImages++
		sum.ExpiredImageSize += aws.ToInt64(d.ImageSizeInBytes)
	}

	if sociIds, err := app.findSociIndex(ctx, repo, lo.Keys(expiredManifests)); err != nil {
		return nil, sum, err
	} else {
		sum.expiredSociIndexes += int64(len(sociIds))
		expiredIds = append(expiredIds, sociIds...)
	}

	if sum.expiredManifests > 0 {
		log.Printf("[notice] %s expired manifests: %d", repo, sum.expiredManifests)
	}
	if sum.expiredSociIndexes > 0 {
		log.Printf("[notice] %s expired soci indexes: %d", repo, sum.expiredSociIndexes)
	}

	return expiredIds, sum, nil
}

func (app *App) listImageDetails(ctx context.Context, repo string) ([]ecrTypes.ImageDetail, map[string]struct{}, error) {
	details := make([]ecrTypes.ImageDetail, 0)
	foundTags := make(map[string]struct{}, 0)

	p := ecr.NewDescribeImagesPaginator(app.ecr, &ecr.DescribeImagesInput{
		RepositoryName: &repo,
	})
	for p.HasMorePages() {
		imgs, err := p.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, img := range imgs.ImageDetails {
			for _, tag := range img.ImageTags {
				foundTags[tag] = struct{}{}
			}
			// adds only container images (not manifests or soci indexes or etc...)
			mediaType := ociTypes.MediaType(aws.ToString(img.ArtifactMediaType))
			switch mediaType {
			case ociTypes.DockerConfigJSON, ociTypes.OCIConfigJSON:
				details = append(details, img)
			default:
				log.Printf(
					"[debug] Skipping non container image: mediatype:%s digest:%s",
					mediaType,
					*img.ImageDigest,
				)
			}
		}
	}
	sort.SliceStable(details, func(i, j int) bool {
		return details[i].ImagePushedAt.After(*details[j].ImagePushedAt)
	})
	return details, foundTags, nil
}

func (app *App) findSociIndex(ctx context.Context, repo string, imageTags []string) ([]ecrTypes.ImageIdentifier, error) {
	ids := make([]ecrTypes.ImageIdentifier, 0, len(imageTags))

	for _, c := range lo.Chunk(imageTags, batchGetImageLimit) {
		imageIds := make([]ecrTypes.ImageIdentifier, 0, len(c))
		for _, tag := range c {
			imageIds = append(imageIds, ecrTypes.ImageIdentifier{ImageTag: aws.String(tag)})
		}
		res, err := app.ecr.BatchGetImage(ctx, &ecr.BatchGetImageInput{
			ImageIds:       imageIds,
			RepositoryName: &repo,
		})
		if err != nil {
			return nil, err
		}
		for _, img := range res.Images {
			if img.ImageManifest == nil {
				continue
			}
			var m oci.IndexManifest
			if err := json.Unmarshal([]byte(*img.ImageManifest), &m); err != nil {
				log.Printf("[warn] failed to parse manifest: %s %s", *img.ImageId.ImageDigest, err)
				continue
			}
			for _, d := range m.Manifests {
				if d.ArtifactType == "application/vnd.amazon.soci.index.v1+json" {
					log.Printf("[notice] %s:%s is expired (soci index)", repo, d.Digest.String())
					ids = append(ids, ecrTypes.ImageIdentifier{ImageDigest: aws.String(d.Digest.String())})
				}
			}
		}
	}
	return ids, nil
}

func imageTag(d ecrTypes.ImageDetail) (string, bool) {
	if len(d.ImageTags) > 1 {
		return "{" + strings.Join(d.ImageTags, ",") + "}", true
	} else if len(d.ImageTags) == 1 {
		return d.ImageTags[0], true
	} else {
		return untaggedStr, false
	}
}

func (app *App) scanClusters(ctx context.Context, clustersConfigs []*ClusterConfig) ([]taskdef, error) {
	tds := make([]taskdef, 0)
	clusterArns, err := app.clusterArns(ctx)
	if err != nil {
		return tds, err
	}

	for _, a := range clusterArns {
		var clusterArn string
		for _, cc := range clustersConfigs {
			if cc.Match(a) {
				clusterArn = a
				break
			}
		}
		if clusterArn == "" {
			continue
		}

		log.Printf("[debug] Checking cluster %s", clusterArn)
		_tds, err := app.availableTaskDefinitionsInCluster(ctx, clusterArn)
		if err != nil {
			return tds, err
		}
		tds = append(tds, _tds...)
	}
	return tds, nil
}

func (app *App) scanTaskdefs(ctx context.Context, tcs []*TaskdefConfig) ([]taskdef, error) {
	tds := make([]taskdef, 0)
	famiries, err := app.taskDefinitionFamilies(ctx)
	if err != nil {
		return tds, err
	}

	for _, family := range famiries {
		var name string
		var keepCount int64
		for _, tc := range tcs {
			if tc.Match(family) {
				name = family
				keepCount = tc.KeepCount
				break
			}
		}
		if name == "" {
			continue
		}
		log.Printf("[debug] Checking task definitions %s latest %d revisions", name, keepCount)
		res, err := app.ecs.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
			FamilyPrefix: &name,
			MaxResults:   aws.Int32(int32(keepCount)),
			Sort:         ecsTypes.SortOrderDesc,
		})
		if err != nil {
			return tds, err
		}
		for _, tdArn := range res.TaskDefinitionArns {
			an, _ := arn.Parse(tdArn)
			r := strings.Replace(an.Resource, "task-definition/", "", 1)
			p := strings.SplitN(r, ":", 2)
			rev, _ := strconv.Atoi(p[1])
			tds = append(tds, taskdef{
				name:     p[0],
				revision: rev,
			})
		}
	}
	return tds, nil
}

func (app App) extractECRImages(ctx context.Context, tdName string) ([]string, error) {
	images := make([]string, 0)
	out, err := app.ecs.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &tdName,
	})
	if err != nil {
		return nil, err
	}
	for _, container := range out.TaskDefinition.ContainerDefinitions {
		img := *container.Image
		if strings.Contains(img, ".dkr.ecr.") {
			images = append(images, *container.Image)
		} else {
			log.Printf("[debug] Skipping non ECR image %s", img)
		}
	}
	return images, nil
}

func (app *App) availableTaskDefinitionsInCluster(ctx context.Context, clusterArn string) ([]taskdef, error) {
	clusterName := clusterArnToName(clusterArn)
	taskDefs := make(map[string]struct{})
	log.Printf("[debug] Checking tasks in %s", clusterArn)
	tp := ecs.NewListTasksPaginator(app.ecs, &ecs.ListTasksInput{Cluster: &clusterArn})
	for tp.HasMorePages() {
		to, err := tp.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		if len(to.TaskArns) == 0 {
			continue
		}
		tasks, err := app.ecs.DescribeTasks(ctx, &ecs.DescribeTasksInput{
			Cluster: &clusterArn,
			Tasks:   to.TaskArns,
		})
		if err != nil {
			return nil, err
		}
		for _, task := range tasks.Tasks {
			td := strings.Split(*task.TaskDefinitionArn, "/")[1]
			if _, found := taskDefs[td]; !found {
				log.Printf("[info] %s is used by tasks in %s", td, clusterName)
				taskDefs[td] = struct{}{}
			}
		}
	}

	sp := ecs.NewListServicesPaginator(app.ecs, &ecs.ListServicesInput{Cluster: &clusterArn})
	for sp.HasMorePages() {
		so, err := sp.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		if len(so.ServiceArns) == 0 {
			continue
		}
		svs, err := app.ecs.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  &clusterArn,
			Services: so.ServiceArns,
		})
		if err != nil {
			return nil, err
		}
		for _, sv := range svs.Services {
			log.Printf("[debug] Checking service %s", *sv.ServiceName)
			for _, dp := range sv.Deployments {
				td := strings.Split(*dp.TaskDefinition, "/")[1]
				if _, found := taskDefs[td]; !found {
					log.Printf("[info] %s is used by %s deployment on %s/%s", td, *dp.Status, *sv.ServiceName, clusterName)
					taskDefs[td] = struct{}{}
				}
			}
		}
	}
	var tds []taskdef
	for td := range taskDefs {
		p := strings.SplitN(td, ":", 2)
		name := p[0]
		rev, _ := strconv.Atoi(p[1])
		tds = append(tds, taskdef{name: name, revision: rev})
	}
	return tds, nil
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
