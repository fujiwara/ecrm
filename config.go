package ecrm

import (
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/fujiwara/ecrm/wildcard"
	"github.com/goccy/go-yaml"
	"github.com/k1LoW/duration"
)

type Config struct {
	Clusters     []*ClusterConfig    `yaml:"clusters"`
	Repositories []*RepositoryConfig `yaml:"repositories"`
}

func (c *Config) Validate() error {
	for _, cluster := range c.Clusters {
		if err := cluster.Validate(); err != nil {
			return err
		}
	}
	for _, repository := range c.Repositories {
		if err := repository.Validate(); err != nil {
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
	if arn.IsARN(name) {
		a, _ := arn.Parse(name)
		name = strings.Replace(a.Resource, "cluster/", "", 1)
	}
	if c.Name == name {
		return true
	}
	return wildcard.Match(c.NamePattern, name)
}

type RepositoryConfig struct {
	Name            string   `yaml:"name"`
	NamePattern     string   `yaml:"name_pattern"`
	Expires         string   `yaml:"expires"`
	KeepTagPatterns []string `yaml:"keep_tag_patterns"`

	expireBefore time.Time
}

func (r *RepositoryConfig) Validate() error {
	now := time.Now()
	if r.Name == "" && r.NamePattern == "" {
		return errors.New("repository name or name_pattern is required")
	}
	if r.Expires != "" {
		if d, err := duration.Parse(r.Expires); err != nil {
			return err
		} else {
			r.expireBefore = now.Add(-d)
		}
	} else {
		r.expireBefore = now.Add(-DefaultExpires)
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
