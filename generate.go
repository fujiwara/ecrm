package ecrm

import (
	"context"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/urfave/cli/v2"
)

var nameToPatternRe = regexp.MustCompile(`^.*?[/_-]`)

func nameToPattern(s string) string {
	p := nameToPatternRe.FindAllString(s, 1)
	if len(p) > 0 {
		return p[0] + "*"
	}
	return s
}

func (app *App) GenerateConfig(ctx context.Context, configFile string, opt Option) error {
	config := Config{}
	if err := app.generateClusterConfig(ctx, &config); err != nil {
		return err
	}
	if err := app.generateTaskdefConfig(ctx, &config); err != nil {
		return err
	}
	if err := app.generateLambdaConfig(ctx, &config); err != nil {
		return err
	}
	if err := app.generateRepositoryConfig(ctx, &config); err != nil {
		return err
	}

	return yaml.NewEncoder(os.Stdout, yaml.IndentSequence(true)).Encode(config)
}

func (app *App) generateClusterConfig(ctx context.Context, config *Config) error {
	clusters, err := app.clusterArns(ctx)
	if err != nil {
		return err
	}
	clusterNames := make(map[string]struct{}, len(clusters))
	for _, c := range clusters {
		name := clusterArnToName(c)
		pattern := nameToPattern(name)
		clusterNames[pattern] = struct{}{}
		log.Printf("[debug] cluster %s -> %s", name, pattern)
	}
	for name := range clusterNames {
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

func (app *App) generateTaskdefConfig(ctx context.Context, config *Config) error {
	taskdefs, err := app.taskDefinitionFamilies(ctx)
	if err != nil {
		return err
	}
	taskdefNames := make(map[string]struct{}, len(taskdefs))
	for _, n := range taskdefs {
		name := arnToName(n, "")
		pattern := nameToPattern(name)
		taskdefNames[pattern] = struct{}{}
		log.Printf("[debug] taskdef %s -> %s", name, pattern)
	}
	for name := range taskdefNames {
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

func (app *App) generateLambdaConfig(ctx context.Context, config *Config) error {
	lambdas, err := app.lambdaFunctions(ctx)
	if err != nil {
		return err
	}
	lambdaNames := make(map[string]struct{}, len(lambdas))
	for _, c := range lambdas {
		name := arnToName(*c.FunctionName, "")
		pattern := nameToPattern(name)
		lambdaNames[pattern] = struct{}{}
		log.Printf("[debug] lambda %s -> %s", name, pattern)
	}
	for name := range lambdaNames {
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

func (app *App) generateRepositoryConfig(ctx context.Context, config *Config) error {
	repos, err := app.repositories(ctx)
	if err != nil {
		return err
	}
	repoNames := make(map[string]struct{}, len(repos))
	for _, r := range repos {
		name := arnToName(*r.RepositoryName, "")
		pattern := nameToPattern(name)
		repoNames[pattern] = struct{}{}
		log.Printf("[debug] ECR %s -> %s", name, pattern)
	}
	for name := range repoNames {
		cfg := RepositoryConfig{
			KeepCount:       int64(DefaultKeepCount),
			Expires:         DefaultExpiresStr,
			KeepTagPatterns: DefaultKeepTagPatterns,
		}
		if strings.Contains(name, "*") {
			cfg.NamePattern = name
		} else {
			cfg.Name = name
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

func (app *App) NewGenerateCommand() *cli.Command {
	return &cli.Command{
		Name:  "generate",
		Usage: "Genarete ecrm.yaml",
		Action: func(c *cli.Context) error {
			return app.GenerateConfig(
				c.Context,
				c.String("config"),
				Option{
					NoColor: c.Bool("no-color"),
				},
			)
		},
	}
}
