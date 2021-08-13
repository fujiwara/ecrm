package ecrm

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

type App struct {
	ctx context.Context
	ecr *ecr.Client
	ecs *ecs.Client
}

type taskdef struct {
	name string
	rev  int
}

func (td taskdef) String() string {
	return fmt.Sprintf("%s:%d", td.name, td.rev)
}

type aliveTags struct {
	RepositoryName string   `json:"repositoryName"`
	Tags           []string `json:"tags"`
}

type Command struct {
	RepositoryName    string
	Keeps             int
	DeregisterTaskDef bool
}

func New(ctx context.Context, region string) (*App, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &App{
		ctx: ctx,
		ecr: ecr.NewFromConfig(cfg),
		ecs: ecs.NewFromConfig(cfg),
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
	clusterArns, err := app.clusterArns()
	if err != nil {
		return err
	}

	tds := make([]taskdef, 0)
	for _, clusterArn := range clusterArns {
		log.Printf("[info] Checking cluster %s", clusterArn)
		_tds, err := app.availableTaskDefinitions(clusterArn)
		if err != nil {
			return err
		}
		tds = append(tds, _tds...)
	}

	availableTds := make([]string, 0)
	outdatedTds := make([]string, 0)
	for _, minTd := range minimumTaskDefinitions(tds) {
		fmt.Println("mininum taskdef", minTd)
		tdp := ecs.NewListTaskDefinitionsPaginator(app.ecs, &ecs.ListTaskDefinitionsInput{
			FamilyPrefix: &minTd.name,
		})
		for tdp.HasMorePages() {
			tdo, err := tdp.NextPage(app.ctx)
			if err != nil {
				return err
			}
			for _, tdArn := range tdo.TaskDefinitionArns {
				r := strings.Split(tdArn, ":")
				rev, _ := strconv.Atoi(r[len(r)-1])
				if rev < minTd.rev-c.Keeps {
					log.Println("[info] Outdated", tdArn)
					outdatedTds = append(outdatedTds, tdArn)
				} else {
					log.Println("[info] Available", tdArn)
					availableTds = append(availableTds, tdArn)
				}
			}
		}
	}

	removableImages := make(map[string]bool)
	for _, tdArn := range outdatedTds {
		imgs, err := app.extractECRImages(tdArn)
		if err != nil {
			return err
		}
		for _, img := range imgs {
			log.Printf("[info] %s may be outdated", img)
			removableImages[img] = true
		}
	}
	for _, tdArn := range availableTds {
		imgs, err := app.extractECRImages(tdArn)
		if err != nil {
			return err
		}
		for _, img := range imgs {
			log.Printf("[info] %s is in use", img)
			removableImages[img] = false
		}
	}
	for img, ok := range removableImages {
		if ok {
			log.Println("[info] Removing image", img)
		}
	}
	return nil
}

func (app App) extractECRImages(tdArn string) ([]string, error) {
	images := make([]string, 0)
	out, err := app.ecs.DescribeTaskDefinition(app.ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &tdArn,
	})
	if err != nil {
		return nil, err
	}
	for _, container := range out.TaskDefinition.ContainerDefinitions {
		img := *container.Image
		if strings.Contains(img, ".dkr.ecr.") {
			images = append(images, *container.Image)
		}
	}
	return images, nil
}

func minimumTaskDefinitions(tds []taskdef) []taskdef {
	lowestTdRev := make(map[string]int)
	for _, td := range tds {
		if _, found := lowestTdRev[td.name]; !found {
			lowestTdRev[td.name] = td.rev
		} else if td.rev < lowestTdRev[td.name] {
			lowestTdRev[td.name] = td.rev
		}
	}
	var res []taskdef
	for name, rev := range lowestTdRev {
		res = append(res, taskdef{name: name, rev: rev})
	}
	return res
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
			td := strings.Split(*sv.TaskDefinition, "/")[1]
			if _, found := taskDefs[td]; !found {
				log.Printf("[info] Found taskDefinition %s in service %s", td, *sv.ServiceName)
				taskDefs[td] = struct{}{}
			}
		}
	}
	var tds []taskdef
	for td := range taskDefs {
		p := strings.SplitN(td, ":", 2)
		name := p[0]
		rev, _ := strconv.Atoi(p[1])
		tds = append(tds, taskdef{name, rev})
	}
	return tds, nil
}
