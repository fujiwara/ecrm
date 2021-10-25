package ecrm

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/fujiwara/ecrm/wildcard"
	"github.com/goccy/go-yaml"
	"github.com/k1LoW/duration"
)

var (
	DefaultKeepCount       = 5
	DefaultKeepTagPatterns = []string{"latest"}
)

type Config struct {
	Clusters        []*ClusterConfig    `yaml:"clusters"`
	TaskDefinitions []*TaskdefConfig    `yaml:"task_definitions"`
	LambdaFunctions []*LambdaConfig     `yaml:"lambda_functions"`
	Repositories    []*RepositoryConfig `yaml:"repositories"`
}

func (c *Config) Validate() error {
	if len(c.Clusters) == 0 {
		log.Println("[warn] clusters are not defined. No ECS clusters will be scanned to find images now using.")
	}
	for _, cc := range c.Clusters {
		if err := cc.Validate(); err != nil {
			return err
		}
	}

	if len(c.TaskDefinitions) == 0 {
		log.Println("[warn] task_definitions are not defined. No task definitions will be scanned to find images now using.")
	}
	for _, tc := range c.TaskDefinitions {
		if err := tc.Validate(); err != nil {
			return err
		}
	}

	if len(c.LambdaFunctions) == 0 {
		log.Println("[warn] lambda_functions are not defined. No Lambda functions will be scanned to find using images.")
	}
	for _, lc := range c.LambdaFunctions {
		if err := lc.Validate(); err != nil {
			return err
		}
	}
	for _, rc := range c.Repositories {
		if err := rc.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type ClusterConfig struct {
	Name        string `yaml:"name"`
	NamePattern string `yaml:"name_pattern"`
}

func (c *ClusterConfig) Validate() error {
	if c.Name == "" && c.NamePattern == "" {
		return errors.New("cluster name or name_pattern is required")
	}
	return nil
}

func (c *ClusterConfig) Match(name string) bool {
	name = clusterArnToName(name)
	if c.Name == name {
		return true
	}
	return wildcard.Match(c.NamePattern, name)
}

type RepositoryConfig struct {
	Name            string   `yaml:"name"`
	NamePattern     string   `yaml:"name_pattern"`
	Expires         string   `yaml:"expires"`
	KeepCount       int64    `yaml:"keep_count"`
	KeepTagPatterns []string `yaml:"keep_tag_patterns"`

	expireBefore time.Time
}

func (r *RepositoryConfig) Validate() error {
	now := time.Now()
	if r.Name != "" && r.NamePattern != "" {
		return errors.New("repositories name and name_pattern are exclusive")
	}
	if r.Expires != "" {
		if d, err := duration.Parse(r.Expires); err != nil {
			return err
		} else {
			r.expireBefore = now.Add(-d)
		}
	} else {
		return fmt.Errorf("repository %s%s expires is required", r.Name, r.NamePattern)
	}

	if len(r.KeepTagPatterns) == 0 {
		log.Printf(
			"[warn] keep_tag_patterns are not defind. set default keep_tag_patterns to %v",
			DefaultKeepTagPatterns,
		)
		r.KeepTagPatterns = DefaultKeepTagPatterns
	}

	return nil
}

func (r *RepositoryConfig) MatchName(name string) bool {
	if r.Name == name {
		return true
	}
	return wildcard.Match(r.NamePattern, name)
}

func (r *RepositoryConfig) MatchTag(tag string) bool {
	for _, pattern := range r.KeepTagPatterns {
		if wildcard.Match(pattern, tag) {
			return true
		}
	}
	return false
}

func (r *RepositoryConfig) IsExpired(at time.Time) bool {
	return at.Before(r.expireBefore)
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

type TaskdefConfig struct {
	Name        string `yaml:"name"`
	NamePattern string `yaml:"name_pattern"`
	KeepCount   int64  `yaml:"keep_count"`
}

func (c *TaskdefConfig) Validate() error {
	if c.Name != "" && c.NamePattern != "" {
		return errors.New("task_definitions name and name_pattern are exclusive")
	}

	if c.KeepCount == 0 {
		log.Printf(
			"[warn] keep_count for task definition %s%s is not defined. set default keep_count to %d",
			c.Name,
			c.NamePattern,
			DefaultKeepCount,
		)
		c.KeepCount = int64(DefaultKeepCount)
	}
	return nil
}

func (c *TaskdefConfig) Match(name string) bool {
	if c.Name == name {
		return true
	}
	return wildcard.Match(c.NamePattern, name)
}

type LambdaConfig struct {
	Name        string `yaml:"name"`
	NamePattern string `yaml:"name_pattern"`
	KeepCount   int64  `yaml:"keep_count"`
	KeepAliase  bool   `yaml:"keep_aliase"`
}

func (c *LambdaConfig) Validate() error {
	if c.Name != "" && c.NamePattern != "" {
		return errors.New("lambda_functions name and name_pattern are exclusive")
	}
	if c.KeepCount == 0 {
		log.Printf(
			"[warn] keep_count for lambda_functions %s%s is not defined. Using default keep_count=%d",
			c.Name,
			c.NamePattern,
			DefaultKeepCount,
		)
		c.KeepCount = int64(DefaultKeepCount)
	}
	return nil
}

func (c *LambdaConfig) Match(name string) bool {
	if c.Name == name {
		return true
	}
	return wildcard.Match(c.NamePattern, name)
}
