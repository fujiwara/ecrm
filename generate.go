package ecrm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Songmu/prompter"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/goccy/go-yaml"
)

var nameToPatternRe = regexp.MustCompile(`^.*?[/_-]`)

func nameToPattern(s string) string {
	p := nameToPatternRe.FindAllString(s, 1)
	if len(p) > 0 {
		return p[0] + "*"
	}
	return s
}

type Generator struct {
	awsCfg aws.Config
}

func NewGenerator(cfg aws.Config) *Generator {
	return &Generator{
		awsCfg: cfg,
	}
}

func (g *Generator) GenerateConfig(ctx context.Context, configFile string) error {
	config := Config{}
	if err := g.generateClusterConfig(ctx, &config); err != nil {
		return err
	}
	if err := g.generateTaskdefConfig(ctx, &config); err != nil {
		return err
	}
	if err := g.generateLambdaConfig(ctx, &config); err != nil {
		return err
	}
	if err := g.generateRepositoryConfig(ctx, &config); err != nil {
		return err
	}

	buf := bytes.NewBuffer(nil)
	if err := yaml.NewEncoder(buf, yaml.IndentSequence(true)).Encode(config); err != nil {
		return err
	}
	log.Println("[notice] Generated config:")
	os.Stderr.Write(buf.Bytes())

	if _, err := os.Stat(configFile); err == nil {
		if !prompter.YN(fmt.Sprintf("%s file already exists. Overwrite?", configFile), false) {
			return errors.New("aborted")
		}
	}
	if err := os.WriteFile(configFile, buf.Bytes(), 0644); err != nil {
		return err
	}
	log.Println("[notice] Saved", configFile)
	return nil
}

func (g *Generator) generateClusterConfig(ctx context.Context, config *Config) error {
	clusters, err := clusterArns(ctx, ecs.NewFromConfig(g.awsCfg))
	if err != nil {
		return err
	}
	clusterNames := newSet()
	for _, c := range clusters {
		name := clusterArnToName(c)
		pattern := nameToPattern(name)
		clusterNames.add(pattern)
		log.Printf("[debug] cluster %s -> %s", name, pattern)
	}
	for _, name := range clusterNames.members() {
		cfg := ClusterConfig{}
		if strings.Contains(name, "*") {
			cfg.NamePattern = name
		} else {
			cfg.Name = name
		}
		config.Clusters = append(config.Clusters, &cfg)
	}
	sort.Slice(config.Clusters, func(i, j int) bool {
		if config.Clusters[i].Name != config.Clusters[j].Name {
			return config.Clusters[i].Name < config.Clusters[j].Name
		}
		return config.Clusters[i].NamePattern < config.Clusters[j].NamePattern
	})
	return nil
}

func (g *Generator) generateTaskdefConfig(ctx context.Context, config *Config) error {
	taskdefs, err := taskDefinitionFamilies(ctx, ecs.NewFromConfig(g.awsCfg))
	if err != nil {
		return err
	}
	taskdefNames := newSet()
	for _, n := range taskdefs {
		name := arnToName(n, "")
		pattern := nameToPattern(name)
		taskdefNames.add(pattern)
		log.Printf("[debug] taskdef %s -> %s", name, pattern)
	}
	for _, name := range taskdefNames.members() {
		cfg := TaskdefConfig{
			KeepCount: int64(DefaultKeepCount),
		}
		if strings.Contains(name, "*") {
			cfg.NamePattern = name
		} else {
			cfg.Name = name
		}
		config.TaskDefinitions = append(config.TaskDefinitions, &cfg)
	}
	sort.Slice(config.TaskDefinitions, func(i, j int) bool {
		if config.TaskDefinitions[i].Name != config.TaskDefinitions[j].Name {
			return config.TaskDefinitions[i].Name < config.TaskDefinitions[j].Name
		}
		return config.TaskDefinitions[i].NamePattern < config.TaskDefinitions[j].NamePattern
	})
	return nil
}

func (g *Generator) generateLambdaConfig(ctx context.Context, config *Config) error {
	lambdas, err := lambdaFunctions(ctx, lambda.NewFromConfig(g.awsCfg))
	if err != nil {
		return err
	}
	lambdaNames := newSet()
	for _, c := range lambdas {
		name := arnToName(*c.FunctionName, "")
		pattern := nameToPattern(name)
		lambdaNames.add(pattern)
		log.Printf("[debug] lambda %s -> %s", name, pattern)
	}
	for _, name := range lambdaNames.members() {
		cfg := LambdaConfig{
			KeepCount:  int64(DefaultKeepCount),
			KeepAliase: true,
		}
		if strings.Contains(name, "*") {
			cfg.NamePattern = name
		} else {
			cfg.Name = name
		}
		config.LambdaFunctions = append(config.LambdaFunctions, &cfg)
	}
	sort.Slice(config.LambdaFunctions, func(i, j int) bool {
		if config.LambdaFunctions[i].Name != config.LambdaFunctions[j].Name {
			return config.LambdaFunctions[i].Name < config.LambdaFunctions[j].Name
		}
		return config.LambdaFunctions[i].NamePattern < config.LambdaFunctions[j].NamePattern
	})
	return nil
}

func (g *Generator) generateRepositoryConfig(ctx context.Context, config *Config) error {
	repos, err := g.repositories(ctx)
	if err != nil {
		return err
	}
	repoNames := newSet()
	for _, r := range repos {
		name := arnToName(*r.RepositoryName, "")
		pattern := nameToPattern(name)
		repoNames.add(pattern)
		log.Printf("[debug] ECR %s -> %s", name, pattern)
	}
	for _, name := range repoNames.members() {
		cfg := RepositoryConfig{
			KeepCount:       int64(DefaultKeepCount),
			Expires:         DefaultExpiresStr,
			KeepTagPatterns: DefaultKeepTagPatterns,
		}
		if strings.Contains(name, "*") {
			cfg.NamePattern = name
		} else {
			cfg.Name = RepositoryName(name)
		}
		config.Repositories = append(config.Repositories, &cfg)
	}
	sort.Slice(config.Repositories, func(i, j int) bool {
		if config.Repositories[i].Name != config.Repositories[j].Name {
			return config.Repositories[i].Name < config.Repositories[j].Name
		}
		return config.Repositories[i].NamePattern < config.Repositories[j].NamePattern
	})
	return nil
}

func (g *Generator) repositories(ctx context.Context) ([]ecrTypes.Repository, error) {
	repos := make([]ecrTypes.Repository, 0)
	p := ecr.NewDescribeRepositoriesPaginator(ecr.NewFromConfig(g.awsCfg), &ecr.DescribeRepositoriesInput{})
	for p.HasMorePages() {
		repo, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo.Repositories...)
	}
	return repos, nil
}
