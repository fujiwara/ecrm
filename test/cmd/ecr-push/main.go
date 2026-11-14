// ecr-push pushes dummy images to ECR repositories for the end-to-end test.
//
// It calls the ECR API directly, so that it works without a container runtime
// and runs much faster than the equivalent AWS CLI calls.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
)

const (
	mtOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mtOCIIndex       = "application/vnd.oci.image.index.v1+json"
	mtOCIConfig      = "application/vnd.oci.image.config.v1+json"
	mtOCILayer       = "application/vnd.oci.image.layer.v1.tar+gzip"
	mtDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	mtDockerConfig   = "application/vnd.docker.container.image.v1+json"
	mtDockerLayer    = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	mtSociIndex      = "application/vnd.amazon.soci.index.v1+json"
)

const usage = `Usage: ecr-push [flags] REPOSITORY[:TAG[,TAG...]]...

Push a dummy image for each argument, and print its digest to a line.
The repositories must exist. An argument without tags pushes an untagged image.

Flags:
`

func main() {
	var opt option
	flag.BoolVar(&opt.docker, "docker", false, "push Docker v2 images instead of OCI images")
	flag.BoolVar(&opt.index, "index", false, "push multi-arch image indexes with untagged amd64 and arm64 images")
	flag.BoolVar(&opt.soci, "soci", false, "push a soci index of the image (with -index, of its amd64 image)")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 || (opt.docker && opt.index) {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(context.Background(), opt, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "ecr-push:", err)
		os.Exit(1)
	}
}

type option struct {
	docker bool
	index  bool
	soci   bool
}

func run(ctx context.Context, opt option, args []string) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := ecr.NewFromConfig(cfg)
	for _, arg := range args {
		repo, tags := parseRef(arg)
		p := &pusher{ctx: ctx, client: client, repo: repo}
		digest, err := p.push(opt, tags)
		if err != nil {
			return fmt.Errorf("failed to push %s: %w", arg, err)
		}
		fmt.Println(digest)
	}
	return nil
}

// parseRef parses REPOSITORY[:TAG[,TAG...]].
func parseRef(ref string) (repo string, tags []string) {
	repo, t, ok := strings.Cut(ref, ":")
	if !ok || t == "" {
		return repo, nil
	}
	return repo, strings.Split(t, ",")
}

type descriptor struct {
	MediaType    string            `json:"mediaType"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Size         int               `json:"size"`
	Digest       string            `json:"digest"`
	Platform     map[string]string `json:"platform,omitempty"`
}

type manifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	ArtifactType  string       `json:"artifactType,omitempty"`
	Config        *descriptor  `json:"config,omitempty"`
	Layers        []descriptor `json:"layers,omitempty"`
	Manifests     []descriptor `json:"manifests,omitempty"`
	Subject       *descriptor  `json:"subject,omitempty"`
}

type pusher struct {
	ctx    context.Context
	client *ecr.Client
	repo   string
}

// push pushes an image or an image index, and returns its digest.
func (p *pusher) push(opt option, tags []string) (string, error) {
	// The contents of an image depend on its name, to have a unique digest in the repository.
	name := "untagged"
	if len(tags) > 0 {
		name = tags[0]
	}
	var d, sociSubject descriptor
	var err error
	if opt.index {
		var constituents []descriptor
		d, constituents, err = p.pushIndex(name, tags)
		if err == nil {
			sociSubject = constituents[0]
		}
	} else {
		d, err = p.pushImage(name, tags, opt.docker, "amd64")
		sociSubject = d
	}
	if err != nil {
		return "", err
	}
	if opt.soci {
		if err := p.pushSociIndex(sociSubject); err != nil {
			return "", err
		}
	}
	return d.Digest, nil
}

func (p *pusher) uploadBlob(mediaType string, blob []byte) (descriptor, error) {
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(blob))
	up, err := p.client.InitiateLayerUpload(p.ctx, &ecr.InitiateLayerUploadInput{RepositoryName: &p.repo})
	if err != nil {
		return descriptor{}, err
	}
	if _, err := p.client.UploadLayerPart(p.ctx, &ecr.UploadLayerPartInput{
		RepositoryName: &p.repo,
		UploadId:       up.UploadId,
		PartFirstByte:  aws.Int64(0),
		PartLastByte:   aws.Int64(int64(len(blob) - 1)),
		LayerPartBlob:  blob,
	}); err != nil {
		return descriptor{}, err
	}
	if _, err := p.client.CompleteLayerUpload(p.ctx, &ecr.CompleteLayerUploadInput{
		RepositoryName: &p.repo,
		UploadId:       up.UploadId,
		LayerDigests:   []string{digest},
	}); err != nil {
		return descriptor{}, err
	}
	return descriptor{MediaType: mediaType, Size: len(blob), Digest: digest}, nil
}

// putManifest puts the manifest once for each tag. No tags means untagged.
func (p *pusher) putManifest(m manifest, tags []string) (descriptor, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return descriptor{}, err
	}
	imageTags := []*string{nil}
	if len(tags) > 0 {
		imageTags = nil
		for _, tag := range tags {
			imageTags = append(imageTags, &tag)
		}
	}
	var digest string
	for _, tag := range imageTags {
		out, err := p.client.PutImage(p.ctx, &ecr.PutImageInput{
			RepositoryName:         &p.repo,
			ImageManifest:          aws.String(string(body)),
			ImageManifestMediaType: &m.MediaType,
			ImageTag:               tag,
		})
		if err != nil {
			return descriptor{}, err
		}
		digest = aws.ToString(out.Image.ImageId.ImageDigest)
	}
	return descriptor{MediaType: m.MediaType, Size: len(body), Digest: digest}, nil
}

// pushImage pushes a single platform image.
func (p *pusher) pushImage(name string, tags []string, docker bool, arch string) (descriptor, error) {
	configMT, layerMT, manifestMT := mtOCIConfig, mtOCILayer, mtOCIManifest
	if docker {
		configMT, layerMT, manifestMT = mtDockerConfig, mtDockerLayer, mtDockerManifest
	}
	imageConfig, err := json.Marshal(map[string]any{
		"architecture": arch,
		"os":           "linux",
		"config":       map[string]any{"Labels": map[string]string{"name": p.repo + ":" + name}},
	})
	if err != nil {
		return descriptor{}, err
	}
	c, err := p.uploadBlob(configMT, imageConfig)
	if err != nil {
		return descriptor{}, err
	}
	layer := bytes.Repeat(fmt.Appendf(nil, "layer of %s %s %s\n", p.repo, name, arch), 64)
	l, err := p.uploadBlob(layerMT, layer)
	if err != nil {
		return descriptor{}, err
	}
	return p.putManifest(manifest{
		SchemaVersion: 2,
		MediaType:     manifestMT,
		Config:        &c,
		Layers:        []descriptor{l},
	}, tags)
}

// pushIndex pushes a multi-arch image index with untagged constituents.
// It returns the descriptors of the index and the constituents.
func (p *pusher) pushIndex(name string, tags []string) (descriptor, []descriptor, error) {
	m := manifest{SchemaVersion: 2, MediaType: mtOCIIndex}
	for _, arch := range []string{"amd64", "arm64"} {
		d, err := p.pushImage(name, nil, false, arch)
		if err != nil {
			return descriptor{}, nil, err
		}
		d.Platform = map[string]string{"architecture": arch, "os": "linux"}
		m.Manifests = append(m.Manifests, d)
	}
	d, err := p.putManifest(m, tags)
	return d, m.Manifests, err
}

// pushSociIndex pushes a soci index of the image, and an image index tagged as
// sha256-{digest of the image} that refers to the soci index.
func (p *pusher) pushSociIndex(subject descriptor) error {
	subject.Platform = nil
	blob := fmt.Appendf(nil, "soci index of %s@%s", p.repo, subject.Digest)
	c, err := p.uploadBlob(mtSociIndex, blob)
	if err != nil {
		return err
	}
	l, err := p.uploadBlob("application/octet-stream", append(blob, " ztoc"...))
	if err != nil {
		return err
	}
	d, err := p.putManifest(manifest{
		SchemaVersion: 2,
		MediaType:     mtOCIManifest,
		ArtifactType:  mtSociIndex,
		Config:        &c,
		Layers:        []descriptor{l},
		Subject:       &subject,
	}, nil)
	if err != nil {
		return err
	}
	d.ArtifactType = mtSociIndex
	_, err = p.putManifest(manifest{
		SchemaVersion: 2,
		MediaType:     mtOCIIndex,
		Manifests:     []descriptor{d},
	}, []string{strings.Replace(subject.Digest, "sha256:", "sha256-", 1)})
	return err
}
