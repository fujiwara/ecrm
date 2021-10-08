package ecrm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Songmu/prompter"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

type App struct {
	ctx    context.Context
	ecr    *ecr.Client
	ecs    *ecs.Client
	region string
}

type taskdef struct {
	name     string
	revision int
}

func (td taskdef) String() string {
	return fmt.Sprintf("%s:%d", td.name, td.revision)
}

type Command struct {
	Cluster    string
	Repository string
	Expires    time.Duration
	Delete     bool
	Force      bool
}

func New(ctx context.Context, region string) (*App, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
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

func (app *App) Run(c *Command) error {
	images, err := app.Scan(c.Cluster)
	if err != nil {
		return err
	}

	if c.Repository != "" {
		ids, err := app.ImageIdentifiersToPurge(c.Repository, c.Expires, images)
		if err != nil {
			return err
		}
		return app.DeleteImages(c.Repository, ids, c.Delete)
	}

	p := ecr.NewDescribeRepositoriesPaginator(app.ecr, &ecr.DescribeRepositoriesInput{})
	for p.HasMorePages() {
		repos, err := p.NextPage(app.ctx)
		if err != nil {
			return err
		}
		for _, repo := range repos.Repositories {
			ids, err := app.ImageIdentifiersToPurge(*repo.RepositoryName, c.Expires, images)
			if err != nil {
				return err
			}
			if err := app.DeleteImages(*repo.RepositoryName, ids, c.Delete); err != nil {
				return err
			}
		}
	}
	return nil
}

func (app *App) DeleteImages(repo string, ids []types.ImageIdentifier, doDelete bool) error {
	if len(ids) == 0 {
		log.Println("[info] no need to delete images for repo", repo)
		return nil
	}
	if !doDelete {
		log.Printf("[info] To delete expired %d images on %s, set --delete", len(ids), repo)
		return nil
	}
	if !prompter.YN(fmt.Sprintf("Delete %d images on %s?", len(ids), repo), false) {
		return errors.New("aborted")
	}

	for _, id := range ids {
		log.Printf("[info] Deleting %s %s", repo, *id.ImageDigest)
	}
	_, err := app.ecr.BatchDeleteImage(app.ctx, &ecr.BatchDeleteImageInput{
		ImageIds:       ids,
		RepositoryName: &repo,
	})
	if err != nil {
		return err
	}
	log.Printf("[info] Deleted %s %d images", repo, len(ids))
	return err
}

func (app *App) ImageIdentifiersToPurge(name string, expires time.Duration, holdImages map[string]bool) ([]types.ImageIdentifier, error) {
	p := ecr.NewDescribeImagesPaginator(app.ecr, &ecr.DescribeImagesInput{
		RepositoryName: &name,
	})
	ids := make([]types.ImageIdentifier, 0)
	expireTime := time.Now().Add(-expires)
	for p.HasMorePages() {
		imgs, err := p.NextPage(app.ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range imgs.ImageDetails {
			hold := false
			for _, tag := range d.ImageTags {
				imageArn := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", *d.RegistryId, app.region, *d.RepositoryName, tag)
				if holdImages[imageArn] {
					hold = true
					break
				}
			}
			if hold {
				continue
			}
			at := *d.ImagePushedAt
			if at.Before(expireTime) {
				log.Printf(
					"[info] expired %s:{%s} %s pushd:%s",
					*d.RepositoryName,
					strings.Join(d.ImageTags, ","),
					*d.ImageDigest,
					at.Format(time.RFC3339),
				)
				ids = append(ids, types.ImageIdentifier{ImageDigest: d.ImageDigest})
			}
		}
	}
	return ids, nil
}

func (app *App) Scan(cluster string) (map[string]bool, error) {
	var clusterArns []string
	var err error
	if cluster == "" {
		log.Printf("[info] Checking ECS clusters")
		clusterArns, err = app.clusterArns()
		if err != nil {
			return nil, err
		}
	} else {
		clusterArns = append(clusterArns, cluster)
	}

	tds := make([]taskdef, 0)
	for _, clusterArn := range clusterArns {
		log.Printf("[info] Checking cluster %s", clusterArn)
		_tds, err := app.availableTaskDefinitions(clusterArn)
		if err != nil {
			return nil, err
		}
		tds = append(tds, _tds...)
	}

	inUseImages := make(map[string]bool)
	for _, td := range tds {
		imgs, err := app.extractECRImages(td.String())
		if err != nil {
			return nil, err
		}
		for _, img := range imgs {
			log.Printf("[info] %s is in use", img)
			inUseImages[img] = true
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
				log.Printf("[info] Found taskDefinition %s in tasks", td)
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
					log.Printf("[info] Found taskDefinition %s in %s deployment on service %s", td, *dp.Status, *sv.ServiceName)
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
