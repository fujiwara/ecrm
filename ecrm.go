package ecrm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/Songmu/prompter"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/samber/lo"
)

const (
	MediaTypeSociIndex = "application/vnd.amazon.soci.index.v1+json"
)

var untaggedStr = "__UNTAGGED__"

type App struct {
	Version string

	awsCfg aws.Config
	ecr    *ecr.Client
	region string
}

func New(ctx context.Context) (*App, error) {
	cfg, err := awsConfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &App{
		region: cfg.Region,
		awsCfg: cfg,
		ecr:    ecr.NewFromConfig(cfg),
	}, nil
}

func (app *App) Run(ctx context.Context, path string, opt *Option) error {
	if err := opt.Validate(); err != nil {
		return fmt.Errorf("invalid option: %w", err)
	}

	c, err := LoadConfig(path)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	scanner := NewScanner(app.awsCfg)
	if err := scanner.LoadFiles(opt.ScannedFiles); err != nil {
		return fmt.Errorf("failed to load scanned image URIs: %w", err)
	}
	if opt.Scan {
		if err := scanner.Scan(ctx, c); err != nil {
			return fmt.Errorf("failed to scan: %w", err)
		}
	}
	log.Println("[info] total", len(scanner.Images), "image URIs in use")
	if opt.ScanOnly {
		return ShowScanResult(scanner, opt)
	}

	planner := NewPlanner(app.awsCfg)
	sums, candidates, err := planner.Plan(ctx, c.Repositories, scanner.Images, opt.Repository)
	if err != nil {
		return fmt.Errorf("failed to plan: %w", err)
	}
	if err := ShowSummary(sums, opt); err != nil {
		return fmt.Errorf("failed to show summary: %w", err)
	}

	if !opt.Delete {
		return nil
	}
	for _, name := range candidates.RepositoryNames() {
		if err := app.DeleteImages(ctx, name, candidates[name], opt.Force); err != nil {
			return fmt.Errorf("failed to delete images: %w", err)
		}
	}
	return nil
}

func ShowScanResult(s *Scanner, opt *Option) error {
	w, err := opt.OutputWriter()
	if err != nil {
		return fmt.Errorf("failed to open output: %w", err)
	}
	defer w.Close()
	if err := s.Save(w); err != nil {
		return fmt.Errorf("failed to save scanned image URIs: %w", err)
	}
	return nil
}

func ShowSummary(s SummaryTable, opt *Option) error {
	w, err := opt.OutputWriter()
	if err != nil {
		return fmt.Errorf("failed to open output: %w", err)
	}
	defer w.Close()
	return s.Print(w, opt.Format)
}

const batchDeleteImageIdsLimit = 100
const batchGetImageLimit = 100

// DeleteImages deletes images from the repository
func (app *App) DeleteImages(ctx context.Context, repo RepositoryName, ids []ecrTypes.ImageIdentifier, force bool) error {
	if len(ids) == 0 {
		log.Println("[info] no need to delete images on", repo)
		return nil
	}
	if !force {
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

func (app *App) GenerateConfig(ctx context.Context, path string) error {
	g := NewGenerator(app.awsCfg)
	return g.GenerateConfig(ctx, path)
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
