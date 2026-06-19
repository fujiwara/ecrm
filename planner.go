package ecrm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
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
	slices.Sort(names)
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

	// Pre-compute which image indexes should be kept, then register their constituent
	// platform-specific images (e.g. linux/amd64, linux/arm64) into keepImages.
	// This must happen before evaluating individual images so that constituents of
	// a kept image index are not incorrectly marked as expired.
	keptIndexDigests, keptIndexIDs := p.computeKeptImageIndexIDs(rc, keepImages, imageIndexes)
	if err := p.addConstituentImagesToKeep(ctx, repo, keptIndexIDs, keepImages); err != nil {
		return nil, sums, fmt.Errorf("failed to protect constituent images of kept image indexes: %w", err)
	}

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

	for _, d := range imageIndexes {
		log.Printf("[debug] is an image index %s", *d.ImageDigest)
		sums.Add(d)
		if keptIndexDigests.contains(*d.ImageDigest) {
			continue
		}
		log.Printf("[notice] image index %s@%s is expired %s", repo, *d.ImageDigest, d.ImagePushedAt.Format(time.RFC3339))
		sums.Expire(d)
		expiredIds = append(expiredIds, ecrTypes.ImageIdentifier{ImageDigest: d.ImageDigest})
		for _, tag := range d.ImageTags {
			expiredImageIndexes.add(tag)
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

// computeKeptImageIndexIDs determines which image indexes should be kept by applying
// all standard retention criteria (in-use references, tag patterns, expiry, keep_count).
// Returns the set of kept digests (for quick lookup) and their identifiers (for BatchGetImage).
func (p *Planner) computeKeptImageIndexIDs(rc *RepositoryConfig, keepImages Images, imageIndexes []ecrTypes.ImageDetail) (set, []ecrTypes.ImageIdentifier) {
	keptDigests := newSet()
	keptIDs := make([]ecrTypes.ImageIdentifier, 0)
	var keepCount int64

	for _, d := range imageIndexes {
		keep := false
		if isKeptImageIndex(d, p.region, keepImages) {
			keep = true
		} else {
			tagMatched := slices.ContainsFunc(d.ImageTags, rc.MatchTag)
			if tagMatched {
				keep = true
			} else if !rc.IsExpired(*d.ImagePushedAt) {
				keep = true
			} else {
				_, tagged := imageTag(d)
				if tagged {
					keepCount++
					if keepCount <= rc.KeepCount {
						keep = true
					}
				}
			}
		}
		if keep {
			keptDigests.add(*d.ImageDigest)
			keptIDs = append(keptIDs, ecrTypes.ImageIdentifier{ImageDigest: d.ImageDigest})
		}
	}
	return keptDigests, keptIDs
}

// addConstituentImagesToKeep fetches manifests of kept image indexes and adds every
// constituent platform-specific image digest to keepImages so they are not deleted.
func (p *Planner) addConstituentImagesToKeep(ctx context.Context, repo RepositoryName, keptIndexIDs []ecrTypes.ImageIdentifier, keepImages Images) error {
	if len(keptIndexIDs) == 0 {
		return nil
	}

	for _, c := range lo.Chunk(keptIndexIDs, batchGetImageLimit) {
		res, err := p.ecr.BatchGetImage(ctx, &ecr.BatchGetImageInput{
			ImageIds:       c,
			RepositoryName: aws.String(string(repo)),
			AcceptedMediaTypes: []string{
				string(ociTypes.OCIImageIndex),
				string(ociTypes.DockerManifestList),
			},
		})
		if err != nil {
			return fmt.Errorf("failed to batch get image index manifest: %w", err)
		}
		if len(res.Failures) > 0 {
			for _, f := range res.Failures {
				log.Printf("[warn] failed to get image index manifest: %s %s", aws.ToString(f.ImageId.ImageDigest), f.FailureCode)
			}
			return fmt.Errorf("failed to get %d image index manifest(s), aborting to avoid deleting constituent images", len(res.Failures))
		}
		for _, img := range res.Images {
			if img.ImageManifest == nil {
				continue
			}
			var m oci.IndexManifest
			if err := json.Unmarshal([]byte(*img.ImageManifest), &m); err != nil {
				log.Printf("[warn] failed to parse image index manifest for %s: %s", aws.ToString(img.ImageId.ImageDigest), err)
				continue
			}
			for _, d := range m.Manifests {
				if d.ArtifactType == MediaTypeSociIndex {
					continue
				}
				constituentURI := ImageURI(fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s@%s",
					aws.ToString(img.RegistryId), p.region, aws.ToString(img.RepositoryName), d.Digest.String()))
				if keepImages.Add(constituentURI, "image_index_constituent") {
					log.Printf("[info] constituent image %s is added to keep list by parent image index", constituentURI)
				}
			}
		}
	}
	return nil
}

// isKeptImageIndex reports whether an image index is directly referenced in keepImages
// (by digest or by tag), meaning it is in use by ECS tasks / task definitions / lambda functions.
func isKeptImageIndex(d ecrTypes.ImageDetail, region string, keepImages Images) bool {
	imageURISha256 := ImageURI(fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s@%s",
		aws.ToString(d.RegistryId), region, aws.ToString(d.RepositoryName), aws.ToString(d.ImageDigest)))
	if keepImages.Contains(imageURISha256) {
		return true
	}
	for _, tag := range d.ImageTags {
		imageURI := ImageURI(fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s:%s",
			aws.ToString(d.RegistryId), region, aws.ToString(d.RepositoryName), tag))
		if keepImages.Contains(imageURI) {
			return true
		}
	}
	return false
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
