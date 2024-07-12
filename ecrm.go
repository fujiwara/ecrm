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

const (
	MediaTypeSociIndex = "application/vnd.amazon.soci.index.v1+json"
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
	images := make(Images)
	if tds, imgs, err := app.scanClusters(ctx, c.Clusters); err != nil {
		return err
	} else {
		taskdefs = append(taskdefs, tds...)
		images.Merge(imgs)
	}
	if tds, err := app.scanTaskdefs(ctx, c.TaskDefinitions); err != nil {
		return err
	} else {
		taskdefs = append(taskdefs, tds...)
	}
	if imgs, err := app.aggregateECRImages(ctx, taskdefs); err != nil {
		return err
	} else {
		images.Merge(imgs)
	}

	if imgs, err := app.scanLambdaFunctions(ctx, c.LambdaFunctions); err != nil {
		return err
	} else {
		images.Merge(imgs)
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

func (app *App) aggregateECRImages(ctx context.Context, taskdefs []taskdef) (Images, error) {
	images := make(Images)
	dup := newSet()
	for _, td := range taskdefs {
		tds := td.String()
		if dup.contains(tds) {
			continue
		}
		dup.add(tds)

		ids, err := app.extractECRImages(ctx, tds)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			log.Printf("[info] %s is in use by task definition %s", id.Short(), tds)
			images.Add(id, tds)
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

func (app *App) scanRepositories(ctx context.Context, rcs []*RepositoryConfig, images Images, opt Option) (map[RepositoryName][]ecrTypes.ImageIdentifier, error) {
	idsMaps := make(map[RepositoryName][]ecrTypes.ImageIdentifier)
	sums := SummaryTable{}
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
			name := RepositoryName(*repo.RepositoryName)
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
			sums = append(sums, sum...)
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

func (app *App) DeleteImages(ctx context.Context, repo RepositoryName, ids []ecrTypes.ImageIdentifier, opt Option) error {
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
			RepositoryName: aws.String(string(repo)),
		})
		if err != nil {
			return err
		}
		deletedCount += len(output.ImageIds)
	}
	return nil
}

func (app *App) unusedImageIdentifiers(ctx context.Context, repo RepositoryName, rc *RepositoryConfig, holdImages Images) ([]ecrTypes.ImageIdentifier, RepoSummary, error) {
	sums := NewRepoSummary(repo)
	images, imageIndexes, sociIndexes, idByTags, err := app.listImageDetails(ctx, repo)
	if err != nil {
		return nil, sums, err
	}
	log.Printf("[info] %s has %d images, %d image indexes, %d soci indexes", repo, len(images), len(imageIndexes), len(sociIndexes))
	expiredIds := make([]ecrTypes.ImageIdentifier, 0)
	expiredImageIndexes := newSet()
	var keepCount int64
IMAGE:
	for _, d := range images {
		tag, tagged := imageTag(d)
		displayName := string(repo) + ":" + tag
		sums.Add(d)

		// Check if the image is in use (digest)
		imageArnSha256 := ImageID(fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s@%s", *d.RegistryId, app.region, *d.RepositoryName, *d.ImageDigest))
		if holdImages.Contains(imageArnSha256) {
			log.Printf("[info] %s@%s is in used, keep it", repo, *d.ImageDigest)
			continue IMAGE
		}

		// Check if the image is in use or conditions (tag)
		for _, tag := range d.ImageTags {
			if rc.MatchTag(tag) {
				log.Printf("[info] %s:%s is matched by tag condition, keep it", repo, tag)
				continue IMAGE
			}
			imageArn := ImageID(fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", *d.RegistryId, app.region, *d.RepositoryName, tag))
			if holdImages.Contains(imageArn) {
				log.Printf("[info] %s:%s is in used, keep it", repo, tag)
				continue IMAGE
			}
		}

		pushedAt := *d.ImagePushedAt
		if !rc.IsExpired(pushedAt) {
			log.Println("[info]", displayName, "is not expired, keep it")
			continue IMAGE
		}
		if tagged {
			keepCount++
			if keepCount <= rc.KeepCount {
				log.Printf("[info] %s is in keep_count %d <= %d, keep it", displayName, keepCount, rc.KeepCount)
				continue IMAGE
			}
		}

		// Don't match any conditions, so expired
		log.Printf("[notice] %s is expired %s %s", displayName, *d.ImageDigest, pushedAt.Format(time.RFC3339))
		expiredIds = append(expiredIds, ecrTypes.ImageIdentifier{ImageDigest: d.ImageDigest})
		sums.Expire(d)

		tagSha256 := strings.Replace(*d.ImageDigest, "sha256:", "sha256-", 1)
		if _, found := idByTags[tagSha256]; found {
			expiredImageIndexes.add(tagSha256)
		}
	}

IMAGE_INDEX:
	for _, d := range imageIndexes {
		log.Printf("[debug] is an image index %s", *d.ImageDigest)
		sums.Add(d)
		for _, tag := range d.ImageTags {
			if expiredImageIndexes.contains(tag) {
				log.Printf("[notice] %s:%s is expired (image index)", repo, tag)
				sums.Expire(d)
				expiredIds = append(expiredIds, ecrTypes.ImageIdentifier{ImageDigest: d.ImageDigest})
				continue IMAGE_INDEX
			}
		}
	}

	sociIds, err := app.findSociIndex(ctx, repo, expiredImageIndexes.members())
	if err != nil {
		return nil, sums, err
	}

SOCI_INDEX:
	for _, d := range sociIndexes {
		log.Printf("[debug] is soci index %s", *d.ImageDigest)
		sums.Add(d)
		for _, id := range sociIds {
			if aws.ToString(id.ImageDigest) == aws.ToString(d.ImageDigest) {
				log.Printf("[notice] %s@%s is expired (soci index)", repo, *d.ImageDigest)
				sums.Expire(d)
				expiredIds = append(expiredIds, ecrTypes.ImageIdentifier{ImageDigest: d.ImageDigest})
				continue SOCI_INDEX
			}
		}
	}

	return expiredIds, sums, nil
}

func (app *App) listImageDetails(ctx context.Context, repo RepositoryName) ([]ecrTypes.ImageDetail, []ecrTypes.ImageDetail, []ecrTypes.ImageDetail, map[string]ecrTypes.ImageIdentifier, error) {
	var images, imageIndexes, sociIndexes []ecrTypes.ImageDetail
	foundTags := make(map[string]ecrTypes.ImageIdentifier, 0)

	p := ecr.NewDescribeImagesPaginator(app.ecr, &ecr.DescribeImagesInput{
		RepositoryName: aws.String(string(repo)),
	})
	for p.HasMorePages() {
		imgs, err := p.NextPage(ctx)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		for _, img := range imgs.ImageDetails {
			if isContainerImage(img) {
				images = append(images, img)
			} else if isImageIndex(img) {
				imageIndexes = append(imageIndexes, img)
			} else if isSociIndex(img) {
				sociIndexes = append(sociIndexes, img)
			}
			for _, tag := range img.ImageTags {
				foundTags[tag] = ecrTypes.ImageIdentifier{ImageDigest: img.ImageDigest}
			}
		}
	}

	sort.SliceStable(images, func(i, j int) bool {
		return images[i].ImagePushedAt.After(*images[j].ImagePushedAt)
	})
	sort.SliceStable(imageIndexes, func(i, j int) bool {
		return imageIndexes[i].ImagePushedAt.After(*imageIndexes[j].ImagePushedAt)
	})
	sort.SliceStable(sociIndexes, func(i, j int) bool {
		return sociIndexes[i].ImagePushedAt.After(*sociIndexes[j].ImagePushedAt)
	})
	return images, imageIndexes, sociIndexes, foundTags, nil
}

func (app *App) findSociIndex(ctx context.Context, repo RepositoryName, imageTags []string) ([]ecrTypes.ImageIdentifier, error) {
	ids := make([]ecrTypes.ImageIdentifier, 0, len(imageTags))

	for _, c := range lo.Chunk(imageTags, batchGetImageLimit) {
		imageIds := make([]ecrTypes.ImageIdentifier, 0, len(c))
		for _, tag := range c {
			imageIds = append(imageIds, ecrTypes.ImageIdentifier{ImageTag: aws.String(tag)})
		}
		res, err := app.ecr.BatchGetImage(ctx, &ecr.BatchGetImageInput{
			ImageIds:       imageIds,
			RepositoryName: aws.String(string(repo)),
			AcceptedMediaTypes: []string{
				string(ociTypes.OCIManifestSchema1),
				string(ociTypes.DockerManifestSchema1),
				string(ociTypes.DockerManifestSchema2),
			},
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
				log.Printf("[warn] failed to parse manifest: %s %s", *img.ImageManifest, err)
				continue
			}
			for _, d := range m.Manifests {
				if d.ArtifactType == MediaTypeSociIndex {
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

func (app *App) scanClusters(ctx context.Context, clustersConfigs []*ClusterConfig) ([]taskdef, Images, error) {
	tds := make([]taskdef, 0)
	images := make(Images)
	clusterArns, err := app.clusterArns(ctx)
	if err != nil {
		return tds, nil, err
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
		if _tds, _imgs, err := app.availableResourcesInCluster(ctx, clusterArn); err != nil {
			return tds, nil, err
		} else {
			tds = append(tds, _tds...)
			images.Merge(_imgs)
		}
	}
	return tds, nil, nil
}

func (app *App) scanTaskdefs(ctx context.Context, tcs []*TaskdefConfig) ([]taskdef, error) {
	tds := make([]taskdef, 0)
	families, err := app.taskDefinitionFamilies(ctx)
	if err != nil {
		return tds, err
	}

	for _, family := range families {
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
			td, err := parseTaskdefArn(tdArn)
			if err != nil {
				return tds, err
			}
			tds = append(tds, td)
		}
	}
	return tds, nil
}

func (app App) extractECRImages(ctx context.Context, tdName string) ([]ImageID, error) {
	images := make([]ImageID, 0)
	out, err := app.ecs.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &tdName,
	})
	if err != nil {
		return nil, err
	}
	for _, container := range out.TaskDefinition.ContainerDefinitions {
		img := *container.Image
		if strings.Contains(img, ".dkr.ecr.") {
			images = append(images, ImageID(*container.Image))
		} else {
			log.Printf("[debug] Skipping non ECR image %s", img)
		}
	}
	return images, nil
}

func (app *App) availableResourcesInCluster(ctx context.Context, clusterArn string) ([]taskdef, Images, error) {
	clusterName := clusterArnToName(clusterArn)
	tdArns := make(set)
	images := make(Images)

	log.Printf("[debug] Checking tasks in %s", clusterArn)
	tp := ecs.NewListTasksPaginator(app.ecs, &ecs.ListTasksInput{Cluster: &clusterArn})
	for tp.HasMorePages() {
		to, err := tp.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(to.TaskArns) == 0 {
			continue
		}
		tasks, err := app.ecs.DescribeTasks(ctx, &ecs.DescribeTasksInput{
			Cluster: &clusterArn,
			Tasks:   to.TaskArns,
		})
		if err != nil {
			return nil, nil, err
		}
		for _, task := range tasks.Tasks {
			tdArn := aws.ToString(task.TaskDefinitionArn)
			if tdArns.add(tdArn) {
				log.Printf("[info] %s is used by tasks in %s", tdArn, clusterName)
			}
			/*
				for _, c := range task.Containers {
					img := aws.ToString(c.Image)
					if strings.Contains(img, ".dkr.ecr.") {
						images.Add(ImageID(img), tdArn)
						if p := strings.SplitN(img, ":", 2); len(p) == 2 { // image with tag
							base := p[0]
							digest := aws.ToString(c.ImageDigest)
							images.Add(ImageID(base+"@"+digest), tdArn)
						}
					}
				}
			*/
		}
	}

	sp := ecs.NewListServicesPaginator(app.ecs, &ecs.ListServicesInput{Cluster: &clusterArn})
	for sp.HasMorePages() {
		so, err := sp.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(so.ServiceArns) == 0 {
			continue
		}
		svs, err := app.ecs.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  &clusterArn,
			Services: so.ServiceArns,
		})
		if err != nil {
			return nil, nil, err
		}
		for _, sv := range svs.Services {
			log.Printf("[debug] Checking service %s", *sv.ServiceName)
			for _, dp := range sv.Deployments {
				a := aws.ToString(dp.TaskDefinition)
				if tdArns.add(a) {
					log.Printf("[info] %s is used by %s deployment on %s/%s", a, *dp.Status, *sv.ServiceName, clusterName)
				}
			}
		}
	}
	var tds []taskdef
	for a := range tdArns {
		td, err := parseTaskdefArn(a)
		if err != nil {
			return nil, nil, err
		}
		tds = append(tds, td)
	}
	return tds, images, nil
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
