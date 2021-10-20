package ecrm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Songmu/prompter"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/dustin/go-humanize"
	"github.com/olekukonko/tablewriter"
)

var DefaultExpires = time.Hour * 24 * 30 * 12 // 1 year

var untaggedStr = "__UNTAGGED__"

type App struct {
	ctx    context.Context
	ecr    *ecr.Client
	ecs    *ecs.Client
	region string
}

type summary struct {
	repo             string
	expiredImages    int64
	totalImages      int64
	expiredImageSize int64
	totalImageSize   int64
}

func (s *summary) row() []string {
	return []string{
		s.repo,
		fmt.Sprintf("%d (%s)", s.expiredImages, humanize.Bytes(uint64(s.expiredImageSize))),
		fmt.Sprintf("%d (%s)", s.totalImages, humanize.Bytes(uint64(s.totalImageSize))),
	}
}

type summaries []*summary

func (s *summaries) print(w io.Writer) {
	t := tablewriter.NewWriter(w)
	t.SetHeader(s.header())
	for _, s := range *s {
		t.Append(s.row())
	}
	t.Render()
}

func (s *summaries) header() []string {
	return []string{
		"name",
		"expired",
		"total",
	}
}

type taskdef struct {
	name     string
	revision int
}

func (td taskdef) String() string {
	return fmt.Sprintf("%s:%d", td.name, td.revision)
}

type Option struct {
	Delete bool
	Force  bool
}

func New(ctx context.Context, region string) (*App, error) {
	cfg, err := awsConfig.LoadDefaultConfig(ctx, awsConfig.WithRegion(region))
	if err != nil {
		return nil, err
	}

	return &App{
		region: cfg.Region,
		ctx:    ctx,
		ecr:    ecr.NewFromConfig(cfg),
		ecs:    ecs.NewFromConfig(cfg),
	}, nil
}

func (app *App) clusterArns() ([]string, error) {
	clusters := make([]string, 0)
	p := ecs.NewListClustersPaginator(app.ecs, &ecs.ListClustersInput{})
	for p.HasMorePages() {
		co, err := p.NextPage(app.ctx)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, co.ClusterArns...)
	}
	return clusters, nil
}

func (app *App) Run(path string, opt Option) error {
	c, err := LoadConfig(path)
	if err != nil {
		return err
	}

	images, err := app.Scan(c.Clusters)
	if err != nil {
		return err
	}

	var sums summaries
	p := ecr.NewDescribeRepositoriesPaginator(app.ecr, &ecr.DescribeRepositoriesInput{})
	for p.HasMorePages() {
		repos, err := p.NextPage(app.ctx)
		if err != nil {
			return err
		}
	REPO:
		for _, repo := range repos.Repositories {
			name := *repo.RepositoryName
			var rc *RepositoryConfig
			for _, r := range c.Repositories {
				if !r.MatchName(name) {
					continue REPO
				}
				rc = r
				break
			}
			ids, sum, err := app.ImageIdentifiersToPurge(name, rc, images)
			if err != nil {
				return err
			}
			sums = append(sums, sum)
			if err := app.DeleteImages(*repo.RepositoryName, ids, opt); err != nil {
				return err
			}
		}
	}
	if opt.Delete {
		return nil
	}
	sort.SliceStable(sums, func(i, j int) bool {
		return sums[i].repo < sums[j].repo
	})
	sums.print(os.Stdout)
	return nil
}

func (app *App) DeleteImages(repo string, ids []types.ImageIdentifier, opt Option) error {
	if len(ids) == 0 {
		log.Println("[info] no need to delete images on", repo)
		return nil
	}
	if !opt.Delete {
		log.Printf("[notice] Expired %d image(s) on %s exists, run delete command to delete", len(ids), repo)
		return nil
	}
	if !opt.Force {
		if !prompter.YN(fmt.Sprintf("Delete %d images on %s?", len(ids), repo), false) {
			return errors.New("aborted")
		}
	}

	for _, id := range ids {
		log.Printf("[notice] Deleting %s %s", repo, *id.ImageDigest)
	}
	_, err := app.ecr.BatchDeleteImage(app.ctx, &ecr.BatchDeleteImageInput{
		ImageIds:       ids,
		RepositoryName: &repo,
	})
	if err != nil {
		return err
	}
	log.Printf("[info] Deleted %s %d images", repo, len(ids))
	return nil
}

func (app *App) ImageIdentifiersToPurge(name string, rc *RepositoryConfig, holdImages map[string]set) ([]types.ImageIdentifier, *summary, error) {
	sum := &summary{
		repo:             name,
		totalImages:      0,
		expiredImages:    0,
		totalImageSize:   0,
		expiredImageSize: 0,
	}
	p := ecr.NewDescribeImagesPaginator(app.ecr, &ecr.DescribeImagesInput{
		RepositoryName: &name,
	})
	ids := make([]types.ImageIdentifier, 0)
	details := []types.ImageDetail{}
	for p.HasMorePages() {
		imgs, err := p.NextPage(app.ctx)
		if err != nil {
			return nil, sum, err
		}
		details = append(details, imgs.ImageDetails...)
	}
	sort.SliceStable(details, func(i, j int) bool {
		return details[i].ImagePushedAt.After(*details[j].ImagePushedAt)
	})

	var keepCount int64
IMAGE:
	for _, d := range details {
		hold := false
		sum.totalImages++
		sum.totalImageSize += aws.ToInt64(d.ImageSizeInBytes)
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
		displayName := name + ":" + tag
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
		ids = append(ids, types.ImageIdentifier{ImageDigest: d.ImageDigest})
		sum.expiredImages++
		sum.expiredImageSize += aws.ToInt64(d.ImageSizeInBytes)
	}
	return ids, sum, nil
}

func imageTag(d types.ImageDetail) (string, bool) {
	if len(d.ImageTags) > 1 {
		return "{" + strings.Join(d.ImageTags, ",") + "}", true
	} else if len(d.ImageTags) == 1 {
		return d.ImageTags[0], true
	} else {
		return untaggedStr, false
	}
}

func (app *App) Scan(clusters []*ClusterConfig) (map[string]set, error) {
	var clusterArns []string
	var err error
	clusterArns, err = app.clusterArns()
	if err != nil {
		return nil, err
	}

	tds := make([]taskdef, 0)
CLUSTER:
	for _, clusterArn := range clusterArns {
		for _, c := range clusters {
			if !c.Match(clusterArn) {
				continue CLUSTER
			}
		}

		log.Printf("[info] Checking cluster %s", clusterArn)
		_tds, err := app.availableTaskDefinitions(clusterArn)
		if err != nil {
			return nil, err
		}
		tds = append(tds, _tds...)
	}

	inUseImages := make(map[string]set)
	for _, td := range tds {
		imgs, err := app.extractECRImages(td.String())
		if err != nil {
			return nil, err
		}
		for _, img := range imgs {
			log.Printf("[info] %s is in use by %s", img, td.String())
			if inUseImages[img] == nil {
				inUseImages[img] = newSet()
			}
			inUseImages[img].add(td.String())
		}
	}
	return inUseImages, nil
}

func (app App) extractECRImages(tdName string) ([]string, error) {
	images := make([]string, 0)
	out, err := app.ecs.DescribeTaskDefinition(app.ctx, &ecs.DescribeTaskDefinitionInput{
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

func (app *App) availableTaskDefinitions(clusterArn string) ([]taskdef, error) {
	taskDefs := make(map[string]struct{})
	log.Printf("[info] Checking tasks in %s", clusterArn)
	tp := ecs.NewListTasksPaginator(app.ecs, &ecs.ListTasksInput{Cluster: &clusterArn})
	for tp.HasMorePages() {
		to, err := tp.NextPage(app.ctx)
		if err != nil {
			return nil, err
		}
		if len(to.TaskArns) == 0 {
			continue
		}
		tasks, err := app.ecs.DescribeTasks(app.ctx, &ecs.DescribeTasksInput{
			Cluster: &clusterArn,
			Tasks:   to.TaskArns,
		})
		if err != nil {
			return nil, err
		}
		for _, task := range tasks.Tasks {
			td := strings.Split(*task.TaskDefinitionArn, "/")[1]
			if _, found := taskDefs[td]; !found {
				log.Printf("[notice] Found taskDefinition %s in tasks", td)
				taskDefs[td] = struct{}{}
			}
		}
	}

	sp := ecs.NewListServicesPaginator(app.ecs, &ecs.ListServicesInput{Cluster: &clusterArn})
	for sp.HasMorePages() {
		so, err := sp.NextPage(app.ctx)
		if err != nil {
			return nil, err
		}
		if len(so.ServiceArns) == 0 {
			continue
		}
		svs, err := app.ecs.DescribeServices(app.ctx, &ecs.DescribeServicesInput{
			Cluster:  &clusterArn,
			Services: so.ServiceArns,
		})
		if err != nil {
			return nil, err
		}
		for _, sv := range svs.Services {
			log.Printf("[info] Checking service %s", *sv.ServiceName)
			for _, dp := range sv.Deployments {
				td := strings.Split(*dp.TaskDefinition, "/")[1]
				if _, found := taskDefs[td]; !found {
					log.Printf("[notice] Found taskDefinition %s in %s deployment on service %s", td, *dp.Status, *sv.ServiceName)
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
