package ecrm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	oci "github.com/google/go-containerregistry/pkg/v1"
	ociTypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/samber/lo"
)

type Planner struct {
	ecr    *ecr.Client
	region string
}

func NewPlanner(cfg aws.Config) *Planner {
	return &Planner{
		ecr:    ecr.NewFromConfig(cfg),
		region: cfg.Region,
	}
}

type DeletableImageIDs map[RepositoryName][]ecrTypes.ImageIdentifier

func (d DeletableImageIDs) RepositoryNames() []RepositoryName {
	names := lo.Keys(d)
	sort.Slice(names, func(i, j int) bool {
		return names[i] < names[j]
	})
	return names
}

// Plan scans repositories and find expired images, and returns a summary table and a map of deletable image identifiers.
//
// keepImages is a set of images in use by ECS tasks / task definitions / lambda functions
// so that they are not deleted
func (p *Planner) Plan(ctx context.Context, rcs []*RepositoryConfig, keepImages Images, repo RepositoryName) (SummaryTable, DeletableImageIDs, error) {
	idsMaps := make(DeletableImageIDs)
	sums := SummaryTable{}
	in := &ecr.DescribeRepositoriesInput{}
	if repo != "" {
		in.RepositoryNames = []string{string(repo)}
	}
	pager := ecr.NewDescribeRepositoriesPaginator(p.ecr, in)
	for pager.HasMorePages() {
		repos, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to describe repositories: %w", err)
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
			imageIDs, sum, err := p.unusedImageIdentifiers(ctx, name, rc, keepImages)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to find unused image identifiers: %w", err)
			}
			sums = append(sums, sum...)
			idsMaps[name] = imageIDs
		}
	}
	sums.Sort()
	return sums, idsMaps, nil
}

// unusedImageIdentifiers finds image identifiers(by image digests) from the repository.
func (p *Planner) unusedImageIdentifiers(ctx context.Context, repo RepositoryName, rc *RepositoryConfig, keepImages Images) ([]ecrTypes.ImageIdentifier, RepoSummary, error) {
	sums := NewRepoSummary(repo)
	images, imageIndexes, sociIndexes, idByTags, err := p.listImageDetails(ctx, repo)
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
		imageURISha256 := ImageURI(fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s@%s", *d.RegistryId, p.region, *d.RepositoryName, *d.ImageDigest))
		log.Printf("[debug] checking %s", imageURISha256)
		if keepImages.Contains(imageURISha256) {
			log.Printf("[info] %s@%s is in used, keep it", repo, *d.ImageDigest)
			continue IMAGE
		}

		// Check if the image is in use or conditions (tag)
		for _, tag := range d.ImageTags {
			if rc.MatchTag(tag) {
				log.Printf("[info] image %s:%s is matched by tag condition, keep it", repo, tag)
				continue IMAGE
			}
			imageURI := ImageURI(fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s", *d.RegistryId, p.region, *d.RepositoryName, tag))
			log.Printf("[debug] checking %s", imageURI)
			if keepImages.Contains(imageURI) {
				log.Printf("[info] image %s:%s is in used, keep it", repo, tag)
				continue IMAGE
			}
		}

		// Check if the image is expired
		pushedAt := *d.ImagePushedAt
		if !rc.IsExpired(pushedAt) {
			log.Printf("[info] image %s is not expired, keep it", displayName)
			continue IMAGE
		}

		if tagged {
			keepCount++
			if keepCount <= rc.KeepCount {
				log.Printf("[info] image %s is in keep_count %d <= %d, keep it", displayName, keepCount, rc.KeepCount)
				continue IMAGE
			}
		}

		// Don't match any conditions, so expired
		log.Printf("[notice] image %s is expired %s %s", displayName, *d.ImageDigest, pushedAt.Format(time.RFC3339))
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

	sociIds, err := p.findSociIndex(ctx, repo, expiredImageIndexes.members())
	if err != nil {
		return nil, sums, fmt.Errorf("failed to find soci index: %w", err)
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

func (p *Planner) listImageDetails(ctx context.Context, repo RepositoryName) ([]ecrTypes.ImageDetail, []ecrTypes.ImageDetail, []ecrTypes.ImageDetail, map[string]ecrTypes.ImageIdentifier, error) {
	var images, imageIndexes, sociIndexes []ecrTypes.ImageDetail
	foundTags := make(map[string]ecrTypes.ImageIdentifier, 0)

	pager := ecr.NewDescribeImagesPaginator(p.ecr, &ecr.DescribeImagesInput{
		RepositoryName: aws.String(string(repo)),
	})
	for pager.HasMorePages() {
		imgs, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to describe images: %w", err)
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

func (p *Planner) findSociIndex(ctx context.Context, repo RepositoryName, imageTags []string) ([]ecrTypes.ImageIdentifier, error) {
	ids := make([]ecrTypes.ImageIdentifier, 0, len(imageTags))

	for _, c := range lo.Chunk(imageTags, batchGetImageLimit) {
		imageIds := make([]ecrTypes.ImageIdentifier, 0, len(c))
		for _, tag := range c {
			imageIds = append(imageIds, ecrTypes.ImageIdentifier{ImageTag: aws.String(tag)})
		}
		res, err := p.ecr.BatchGetImage(ctx, &ecr.BatchGetImageInput{
			ImageIds:       imageIds,
			RepositoryName: aws.String(string(repo)),
			AcceptedMediaTypes: []string{
				string(ociTypes.OCIManifestSchema1),
				string(ociTypes.DockerManifestSchema1),
				string(ociTypes.DockerManifestSchema2),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to batch get image: %w", err)
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
