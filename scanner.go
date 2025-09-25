package ecrm

import (
	"context"
	"io"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecsTypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/samber/lo"
)

type Scanner struct {
	Images Images

	ecs    *ecs.Client
	eks    *eks.Client
	lambda *lambda.Client
}

func NewScanner(cfg aws.Config) *Scanner {
	return &Scanner{
		Images: make(Images),
		ecs:    ecs.NewFromConfig(cfg),
		eks:    eks.NewFromConfig(cfg),
		lambda: lambda.NewFromConfig(cfg),
	}
}

func (s *Scanner) Scan(ctx context.Context, c *Config) error {
	log.Println("[info] scanning resources")

	// collect images in use by ECS tasks / task definitions
	var taskdefs []taskdef
	if tds, err := s.scanClusters(ctx, c.Clusters); err != nil {
		return err
	} else {
		taskdefs = append(taskdefs, tds...)
	}
	if tds, err := s.collectTaskdefs(ctx, c.TaskDefinitions); err != nil {
		return err
	} else {
		taskdefs = append(taskdefs, tds...)
	}
	if err := s.collectImages(ctx, taskdefs); err != nil {
		return err
	}

	// collect images in use by lambda functions
	if err := s.scanLambdaFunctions(ctx, c.LambdaFunctions); err != nil {
		return err
	}

	if err := s.scanEKSClusters(ctx, c.EKSClusters); err != nil {
		return err
	}

	return nil
}

func (s *Scanner) LoadFiles(files []string) error {
	for _, f := range files {
		log.Println("[info] loading scanned image URIs from", f)
		imgs := make(Images)
		if err := imgs.LoadFile(f); err != nil {
			return err
		}
		log.Println("[info] loaded", len(imgs), "image URIs")
		s.Images.Merge(imgs)
	}
	return nil
}

func (s *Scanner) Save(w io.Writer) error {
	log.Println("[info] saving scanned image URIs")
	if err := s.Images.Print(w); err != nil {
		return err
	}
	log.Println("[info] saved", len(s.Images), "image URIs")
	return nil
}

// collectImages collects images in use by ECS tasks / task definitions
func (s *Scanner) collectImages(ctx context.Context, taskdefs []taskdef) error {
	dup := newSet()
	for _, td := range taskdefs {
		tds := td.String()
		if dup.contains(tds) {
			continue
		}
		dup.add(tds)

		ids, err := s.extractECRImages(ctx, tds)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if s.Images.Add(id, tds) {
				log.Printf("[info] image %s is in use by taskdef %s", id.String(), tds)
			}
		}
	}
	return nil
}

// extractECRImages extracts images (only in ECR) from the task definition
// returns image URIs
func (s *Scanner) extractECRImages(ctx context.Context, tdName string) ([]ImageURI, error) {
	images := make([]ImageURI, 0)
	out, err := s.ecs.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &tdName,
	})
	if err != nil {
		return nil, err
	}
	for _, container := range out.TaskDefinition.ContainerDefinitions {
		u := ImageURI(*container.Image)
		if u.IsECRImage() {
			images = append(images, u)
		} else {
			log.Printf("[debug] Skipping non ECR image %s", u)
		}
	}
	return images, nil
}

// scanClusters scans ECS clusters and returns task definitions and images in use
func (s *Scanner) scanClusters(ctx context.Context, clustersConfigs []*ClusterConfig) ([]taskdef, error) {
	tds := make([]taskdef, 0)
	clusterArns, err := clusterArns(ctx, s.ecs)
	if err != nil {
		return nil, err
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
		if _tds, err := s.availableResourcesInCluster(ctx, clusterArn); err != nil {
			return tds, err
		} else {
			tds = append(tds, _tds...)
		}
	}
	return tds, nil
}

// collectTaskdefs collects task definitions by configurations
func (s *Scanner) collectTaskdefs(ctx context.Context, tcs []*TaskdefConfig) ([]taskdef, error) {
	tds := make([]taskdef, 0)
	families, err := taskDefinitionFamilies(ctx, s.ecs)
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
		res, err := s.ecs.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
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

// availableResourcesInCluster scans task definitions and images in use in the cluster
func (s *Scanner) availableResourcesInCluster(ctx context.Context, clusterArn string) ([]taskdef, error) {
	clusterName := clusterArnToName(clusterArn)
	tdArns := make(set)

	log.Printf("[debug] Checking tasks in %s", clusterArn)
	taskArns := make([]string, 0)
	for _, status := range []ecsTypes.DesiredStatus{ecsTypes.DesiredStatusRunning, ecsTypes.DesiredStatusStopped} {
		tp := ecs.NewListTasksPaginator(s.ecs, &ecs.ListTasksInput{
			Cluster:       &clusterArn,
			DesiredStatus: status,
		})
		for tp.HasMorePages() {
			to, err := tp.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			taskArns = append(taskArns, to.TaskArns...)
		}
	}
	for _, tasks := range lo.Chunk(taskArns, 100) { // 100 is the max for describeTasks API
		tasks, err := s.ecs.DescribeTasks(ctx, &ecs.DescribeTasksInput{
			Cluster: &clusterArn,
			Tasks:   tasks,
		})
		if err != nil {
			return nil, err
		}
		for _, task := range tasks.Tasks {
			tdArn := aws.ToString(task.TaskDefinitionArn)
			td, err := parseTaskdefArn(tdArn)
			if err != nil {
				return nil, err
			}
			ts, err := arn.Parse(*task.TaskArn)
			if err != nil {
				return nil, err
			}
			if tdArns.add(tdArn) {
				log.Printf("[info] taskdef %s is used by %s", td.String(), ts.Resource)
			}
			for _, c := range task.Containers {
				if c.Image == nil {
					continue
				}
				u := ImageURI(aws.ToString(c.Image))
				if !u.IsECRImage() {
					continue
				}
				// ECR image
				if u.IsDigestURI() {
					if s.Images.Add(u, tdArn) {
						log.Printf("[info] image %s is used by %s container on %s", u.String(), *c.Name, ts.Resource)
					}
				} else if c.ImageDigest != nil {
					base := u.Base()
					digest := aws.ToString(c.ImageDigest)
					u := ImageURI(base + "@" + digest)
					if s.Images.Add(u, tdArn) {
						log.Printf("[info] image %s is used by %s container on %s", u.String(), *c.Name, ts.Resource)
					}
				}
			}
		}
	}

	sp := ecs.NewListServicesPaginator(s.ecs, &ecs.ListServicesInput{Cluster: &clusterArn})
	for sp.HasMorePages() {
		so, err := sp.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		if len(so.ServiceArns) == 0 {
			continue
		}
		svs, err := s.ecs.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  &clusterArn,
			Services: so.ServiceArns,
		})
		if err != nil {
			return nil, err
		}
		for _, sv := range svs.Services {
			log.Printf("[debug] Checking service %s", *sv.ServiceName)
			for _, dp := range sv.Deployments {
				tdArn := aws.ToString(dp.TaskDefinition)
				td, err := parseTaskdefArn(tdArn)
				if err != nil {
					return nil, err
				}
				if tdArns.add(tdArn) {
					log.Printf("[info] taskdef %s is used by %s deployment on service %s/%s", td.String(), *dp.Status, *sv.ServiceName, clusterName)
				}
			}
		}
	}
	var tds []taskdef
	for a := range tdArns {
		td, err := parseTaskdefArn(a)
		if err != nil {
			return nil, err
		}
		tds = append(tds, td)
	}
	return tds, nil
}
