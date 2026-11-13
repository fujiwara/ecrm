package ecrm_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecsTypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdaTypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/fujiwara/ecrm"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestIntegration runs scan, plan and delete against AWS Emulator
// (https://github.com/aws/aws-emulator).
//
// It runs only when AWS_ENDPOINT_URL points to a loopback address, so that it
// never touches a real AWS account. Every resource is created with a unique
// prefix and the ecrm configuration is limited to that prefix. That allows the
// test to run repeatedly against the same emulator process.
func TestIntegration(t *testing.T) {
	requireEmulator(t)
	ecrm.SetLogLevel("info")
	ctx := t.Context()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "ecrm-test-" + rand.Text()[:8]
	prefix = strings.ToLower(prefix)
	s := newSeeder(t, ctx, cfg, prefix)
	s.seed()

	// ImagePushedAt cannot be set, so the test uses a short expiration.
	time.Sleep(1500 * time.Millisecond)

	configPath := filepath.Join(t.TempDir(), "ecrm.yaml")
	configYAML := fmt.Sprintf(`
clusters:
  - name_pattern: "%[1]s*"
task_definitions:
  - name_pattern: "%[1]s*"
    keep_count: 2
lambda_functions:
  - name_pattern: "%[1]s*"
    keep_count: 1
repositories:
  - name_pattern: "%[1]s/*"
    expires: 1 sec
    keep_tag_patterns:
      - latest
    keep_count: 1
`, prefix)
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	app, err := ecrm.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, opt *ecrm.Option, v any) {
		t.Helper()
		opt.OutputFile = filepath.Join(t.TempDir(), "output.json")
		if err := app.Run(ctx, configPath, opt); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(opt.OutputFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("failed to parse output: %s\n%s", err, b)
		}
	}
	summaryOpts := []cmp.Option{
		cmpopts.IgnoreFields(ecrm.Summary{}, "ExpiredImageSize", "TotalImageSize"),
	}
	summary := func(repo, typ string, total, expired int64) ecrm.Summary {
		return ecrm.Summary{
			Repo: ecrm.RepositoryName(prefix + "/" + repo), Type: typ, TotalImages: total, ExpiredImages: expired,
		}
	}

	t.Run("scan", func(t *testing.T) {
		var got []string
		run(t, &ecrm.Option{Scan: true, ScanOnly: true}, &got)
		want := []string{
			// in use by the latest 2 task definitions
			s.uri("app/multi", "m2"),
			// in use by a running task (image index by digest, the task definition is out of keep_count)
			s.uri("app/multi", s.digests["app/multi:m1"]),
			// in use by a service (the task definition is out of keep_count)
			s.uri("app/web", "v1"),
			// in use by the latest 2 task definitions
			s.uri("app/web", "v3"),
			s.uri("app/web", "v4"),
			// in use by a lambda function version with an alias
			s.uri("batch/job", "j1"),
			// in use by the latest lambda function version
			s.uri("batch/job", "j2"),
		}
		sort.Strings(want)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("unexpected scan result (-want +got):\n%s", diff)
		}
	})

	t.Run("plan", func(t *testing.T) {
		var got []ecrm.Summary
		run(t, &ecrm.Option{Scan: true, Format: ecrm.FormatJSON}, &got)
		want := []ecrm.Summary{
			// m3 is not in use and out of keep_count. Its constituents and soci index expire together.
			summary("app/multi", ecrm.SummaryTypeImage, 8, 2),
			summary("app/multi", ecrm.SummaryTypeImageIndex, 5, 2),
			summary("app/multi", ecrm.SummaryTypeSociIndex, 1, 1),
			// v2 and an untagged image expire.
			summary("app/web", ecrm.SummaryTypeImage, 7, 2),
			// j3 expires.
			summary("batch/job", ecrm.SummaryTypeImage, 4, 1),
			// o1 and o2 expire.
			summary("unused/old", ecrm.SummaryTypeImage, 3, 2),
		}
		if diff := cmp.Diff(want, got, summaryOpts...); diff != "" {
			t.Errorf("unexpected plan result (-want +got):\n%s", diff)
		}
		// plan must not delete anything
		if diff := cmp.Diff([]string{"j1", "j2", "j3", "j4"}, s.tags("batch/job")); diff != "" {
			t.Errorf("unexpected tags after plan (-want +got):\n%s", diff)
		}
	})

	t.Run("delete", func(t *testing.T) {
		var deleted []ecrm.Summary
		run(t, &ecrm.Option{Scan: true, Delete: true, Force: true, Format: ecrm.FormatJSON}, &deleted)

		wantTags := map[string][]string{
			"app/multi":  {"m1", "m2", "m4"},
			"app/web":    {"latest", "v1", "v3", "v4", "v5", "v6"},
			"batch/job":  {"j1", "j2", "j4"},
			"unused/old": {"o3"},
		}
		for repo, want := range wantTags {
			if diff := cmp.Diff(want, s.tags(repo)); diff != "" {
				t.Errorf("unexpected tags of %s after delete (-want +got):\n%s", repo, diff)
			}
		}

		var got []ecrm.Summary
		run(t, &ecrm.Option{Scan: true, Format: ecrm.FormatJSON}, &got)
		want := []ecrm.Summary{
			summary("app/multi", ecrm.SummaryTypeImage, 6, 0),
			summary("app/multi", ecrm.SummaryTypeImageIndex, 3, 0),
			summary("app/web", ecrm.SummaryTypeImage, 5, 0),
			summary("batch/job", ecrm.SummaryTypeImage, 3, 0),
			summary("unused/old", ecrm.SummaryTypeImage, 1, 0),
		}
		if diff := cmp.Diff(want, got, summaryOpts...); diff != "" {
			t.Errorf("unexpected plan result after delete (-want +got):\n%s", diff)
		}
	})
}

// requireEmulator skips the test unless AWS_ENDPOINT_URL points to a loopback address.
func requireEmulator(t *testing.T) {
	t.Helper()
	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		t.Skip("AWS_ENDPOINT_URL is not set. skip the integration test with AWS Emulator")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("invalid AWS_ENDPOINT_URL: %s", err)
	}
	if host := u.Hostname(); host != "localhost" {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			t.Skipf("AWS_ENDPOINT_URL %s is not a loopback address. skip the integration test", endpoint)
		}
	}
}

const (
	mtOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mtOCIIndex       = "application/vnd.oci.image.index.v1+json"
	mtOCIConfig      = "application/vnd.oci.image.config.v1+json"
	mtOCILayer       = "application/vnd.oci.image.layer.v1.tar+gzip"
	mtDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	mtDockerConfig   = "application/vnd.docker.container.image.v1+json"
	mtDockerLayer    = "application/vnd.docker.image.rootfs.diff.tar.gzip"
)

type testDescriptor struct {
	MediaType    string            `json:"mediaType"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Size         int               `json:"size"`
	Digest       string            `json:"digest"`
	Platform     map[string]string `json:"platform,omitempty"`
}

type testManifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	MediaType     string           `json:"mediaType"`
	ArtifactType  string           `json:"artifactType,omitempty"`
	Config        *testDescriptor  `json:"config,omitempty"`
	Layers        []testDescriptor `json:"layers,omitempty"`
	Manifests     []testDescriptor `json:"manifests,omitempty"`
	Subject       *testDescriptor  `json:"subject,omitempty"`
}

// seeder creates ECR, ECS and Lambda resources for the integration test.
type seeder struct {
	t      *testing.T
	ctx    context.Context
	prefix string
	region string

	ecr    *ecr.Client
	ecs    *ecs.Client
	ec2    *ec2.Client
	lambda *lambda.Client

	registry string
	digests  map[string]string // "repo:tag" => digest
}

func newSeeder(t *testing.T, ctx context.Context, cfg aws.Config, prefix string) *seeder {
	t.Helper()
	id, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		t.Fatal(err)
	}
	return &seeder{
		t:        t,
		ctx:      ctx,
		prefix:   prefix,
		region:   cfg.Region,
		ecr:      ecr.NewFromConfig(cfg),
		ecs:      ecs.NewFromConfig(cfg),
		ec2:      ec2.NewFromConfig(cfg),
		lambda:   lambda.NewFromConfig(cfg),
		registry: fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", aws.ToString(id.Account), cfg.Region),
		digests:  map[string]string{},
	}
}

func (s *seeder) must(err error) {
	s.t.Helper()
	if err != nil {
		s.t.Fatal(err)
	}
}

func (s *seeder) repo(name string) *string {
	return aws.String(s.prefix + "/" + name)
}

// uri returns an image URI for a tag or a digest in the repository.
func (s *seeder) uri(repo, ref string) string {
	sep := ":"
	if strings.HasPrefix(ref, "sha256:") {
		sep = "@"
	}
	return s.registry + "/" + s.prefix + "/" + repo + sep + ref
}

// tags returns all sorted image tags in the repository, except the referrers tags (sha256-...).
func (s *seeder) tags(repo string) []string {
	s.t.Helper()
	tags := []string{}
	p := ecr.NewDescribeImagesPaginator(s.ecr, &ecr.DescribeImagesInput{RepositoryName: s.repo(repo)})
	for p.HasMorePages() {
		out, err := p.NextPage(s.ctx)
		s.must(err)
		for _, d := range out.ImageDetails {
			for _, tag := range d.ImageTags {
				if !strings.HasPrefix(tag, "sha256-") {
					tags = append(tags, tag)
				}
			}
		}
	}
	sort.Strings(tags)
	return tags
}

func (s *seeder) uploadBlob(repo, mediaType string, blob []byte) testDescriptor {
	s.t.Helper()
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(blob))
	up, err := s.ecr.InitiateLayerUpload(s.ctx, &ecr.InitiateLayerUploadInput{RepositoryName: s.repo(repo)})
	s.must(err)
	_, err = s.ecr.UploadLayerPart(s.ctx, &ecr.UploadLayerPartInput{
		RepositoryName: s.repo(repo),
		UploadId:       up.UploadId,
		PartFirstByte:  aws.Int64(0),
		PartLastByte:   aws.Int64(int64(len(blob) - 1)),
		LayerPartBlob:  blob,
	})
	s.must(err)
	_, err = s.ecr.CompleteLayerUpload(s.ctx, &ecr.CompleteLayerUploadInput{
		RepositoryName: s.repo(repo),
		UploadId:       up.UploadId,
		LayerDigests:   []string{digest},
	})
	s.must(err)
	return testDescriptor{MediaType: mediaType, Size: len(blob), Digest: digest}
}

func (s *seeder) putManifest(repo string, m testManifest, tag string) testDescriptor {
	s.t.Helper()
	body, err := json.Marshal(m)
	s.must(err)
	in := &ecr.PutImageInput{
		RepositoryName:         s.repo(repo),
		ImageManifest:          aws.String(string(body)),
		ImageManifestMediaType: aws.String(m.MediaType),
	}
	if tag != "" {
		in.ImageTag = aws.String(tag)
	}
	out, err := s.ecr.PutImage(s.ctx, in)
	s.must(err)
	digest := aws.ToString(out.Image.ImageId.ImageDigest)
	if tag != "" {
		s.digests[repo+":"+tag] = digest
	}
	return testDescriptor{MediaType: m.MediaType, Size: len(body), Digest: digest}
}

// pushImage pushes a single platform image. An empty tag means untagged.
func (s *seeder) pushImage(repo, name, tag string, docker bool, arch string) testDescriptor {
	s.t.Helper()
	configMT, layerMT, manifestMT := mtOCIConfig, mtOCILayer, mtOCIManifest
	if docker {
		configMT, layerMT, manifestMT = mtDockerConfig, mtDockerLayer, mtDockerManifest
	}
	imageConfig, err := json.Marshal(map[string]any{
		"architecture": arch,
		"os":           "linux",
		"config":       map[string]any{"Labels": map[string]string{"name": s.prefix + "/" + repo + ":" + name}},
	})
	s.must(err)
	layer := bytes.Repeat(fmt.Appendf(nil, "layer of %s/%s %s %s\n", s.prefix, repo, name, arch), 64)
	c := s.uploadBlob(repo, configMT, imageConfig)
	return s.putManifest(repo, testManifest{
		SchemaVersion: 2,
		MediaType:     manifestMT,
		Config:        &c,
		Layers:        []testDescriptor{s.uploadBlob(repo, layerMT, layer)},
	}, tag)
}

// pushIndex pushes a multi-arch image index with untagged constituents.
// It returns the descriptors of the constituents.
func (s *seeder) pushIndex(repo, tag string) []testDescriptor {
	s.t.Helper()
	m := testManifest{SchemaVersion: 2, MediaType: mtOCIIndex}
	for _, arch := range []string{"amd64", "arm64"} {
		d := s.pushImage(repo, tag, "", false, arch)
		d.Platform = map[string]string{"architecture": arch, "os": "linux"}
		m.Manifests = append(m.Manifests, d)
	}
	s.putManifest(repo, m, tag)
	return m.Manifests
}

// pushSociIndex pushes a soci index of the image, and an image index tagged as
// sha256-{digest of the image} that refers to the soci index.
func (s *seeder) pushSociIndex(repo string, subject testDescriptor) {
	s.t.Helper()
	subject.Platform = nil
	blob := fmt.Appendf(nil, "soci index of %s/%s@%s", s.prefix, repo, subject.Digest)
	c := s.uploadBlob(repo, ecrm.MediaTypeSociIndex, blob)
	d := s.putManifest(repo, testManifest{
		SchemaVersion: 2,
		MediaType:     mtOCIManifest,
		ArtifactType:  ecrm.MediaTypeSociIndex,
		Config:        &c,
		Layers:        []testDescriptor{s.uploadBlob(repo, "application/octet-stream", append(blob, " ztoc"...))},
		Subject:       &subject,
	}, "")
	d.ArtifactType = ecrm.MediaTypeSociIndex
	s.putManifest(repo, testManifest{
		SchemaVersion: 2,
		MediaType:     mtOCIIndex,
		Manifests:     []testDescriptor{d},
	}, strings.Replace(subject.Digest, "sha256:", "sha256-", 1))
}

func (s *seeder) registerTaskDefinition(family, image string) string {
	s.t.Helper()
	out, err := s.ecs.RegisterTaskDefinition(s.ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(s.prefix + "-" + family),
		RequiresCompatibilities: []ecsTypes.Compatibility{ecsTypes.CompatibilityFargate},
		NetworkMode:             ecsTypes.NetworkModeAwsvpc,
		Cpu:                     aws.String("256"),
		Memory:                  aws.String("512"),
		ContainerDefinitions: []ecsTypes.ContainerDefinition{
			{Name: aws.String("app"), Image: aws.String(image), Essential: aws.Bool(true)},
		},
	})
	s.must(err)
	arn := aws.ToString(out.TaskDefinition.TaskDefinitionArn)
	return arn[strings.LastIndex(arn, "/")+1:] // family:revision
}

func (s *seeder) seed() {
	s.t.Helper()
	s.seedECR()
	s.seedECS()
	s.seedLambda()
}

func (s *seeder) seedECR() {
	s.t.Helper()
	for _, repo := range []string{"app/web", "app/multi", "batch/job", "unused/old"} {
		_, err := s.ecr.CreateRepository(s.ctx, &ecr.CreateRepositoryInput{RepositoryName: s.repo(repo)})
		s.must(err)
	}

	// app/web: single platform images v1..v6 (Docker and OCI), v6 is also tagged as latest.
	for i := 1; i <= 6; i++ {
		tag := fmt.Sprintf("v%d", i)
		s.pushImage("app/web", tag, tag, i%2 == 0, "amd64")
	}
	v6, err := s.ecr.BatchGetImage(s.ctx, &ecr.BatchGetImageInput{
		RepositoryName: s.repo("app/web"),
		ImageIds:       []ecrTypes.ImageIdentifier{{ImageTag: aws.String("v6")}},
	})
	s.must(err)
	_, err = s.ecr.PutImage(s.ctx, &ecr.PutImageInput{
		RepositoryName: s.repo("app/web"),
		ImageTag:       aws.String("latest"),
		ImageManifest:  v6.Images[0].ImageManifest,
	})
	s.must(err)
	s.pushImage("app/web", "untagged", "", false, "amd64")

	// app/multi: image indexes m1..m4. m3 has a soci index for its amd64 image.
	for _, tag := range []string{"m1", "m2", "m3", "m4"} {
		constituents := s.pushIndex("app/multi", tag)
		if tag == "m3" {
			s.pushSociIndex("app/multi", constituents[0])
		}
	}

	for i := 1; i <= 4; i++ {
		tag := fmt.Sprintf("j%d", i)
		s.pushImage("batch/job", tag, tag, false, "amd64")
	}
	for i := 1; i <= 3; i++ {
		tag := fmt.Sprintf("o%d", i)
		s.pushImage("unused/old", tag, tag, false, "amd64")
	}
}

func (s *seeder) seedECS() {
	s.t.Helper()
	cluster := aws.String(s.prefix)
	_, err := s.ecs.CreateCluster(s.ctx, &ecs.CreateClusterInput{ClusterName: cluster})
	s.must(err)

	// web:1..4 use app/web:v1..v4
	var web []string
	for _, tag := range []string{"v1", "v2", "v3", "v4"} {
		web = append(web, s.registerTaskDefinition("web", s.uri("app/web", tag)))
	}
	// multi:1 uses m1 by digest, multi:2..4 use m2 by tag
	multi := s.registerTaskDefinition("multi", s.uri("app/multi", s.digests["app/multi:m1"]))
	for range 3 {
		s.registerTaskDefinition("multi", s.uri("app/multi", "m2"))
	}

	// Fargate tasks require an existing subnet.
	vpc, err := s.ec2.CreateVpc(s.ctx, &ec2.CreateVpcInput{CidrBlock: aws.String("10.0.0.0/16")})
	s.must(err)
	subnet, err := s.ec2.CreateSubnet(s.ctx, &ec2.CreateSubnetInput{
		VpcId:     vpc.Vpc.VpcId,
		CidrBlock: aws.String("10.0.0.0/24"),
	})
	s.must(err)
	network := &ecsTypes.NetworkConfiguration{
		AwsvpcConfiguration: &ecsTypes.AwsVpcConfiguration{Subnets: []string{aws.ToString(subnet.Subnet.SubnetId)}},
	}

	// A service runs web:1, that is out of keep_count of task definitions.
	_, err = s.ecs.CreateService(s.ctx, &ecs.CreateServiceInput{
		Cluster:              cluster,
		ServiceName:          aws.String("web"),
		TaskDefinition:       aws.String(web[0]),
		DesiredCount:         aws.Int32(1),
		LaunchType:           ecsTypes.LaunchTypeFargate,
		NetworkConfiguration: network,
	})
	s.must(err)
	// A standalone task runs multi:1, that is out of keep_count of task definitions.
	task, err := s.ecs.RunTask(s.ctx, &ecs.RunTaskInput{
		Cluster:              cluster,
		TaskDefinition:       aws.String(multi),
		LaunchType:           ecsTypes.LaunchTypeFargate,
		NetworkConfiguration: network,
	})
	s.must(err)
	if len(task.Tasks) != 1 {
		s.t.Fatalf("failed to run a task: %v", task.Failures)
	}
}

func (s *seeder) seedLambda() {
	s.t.Helper()
	name := aws.String(s.prefix + "-job")
	get := &lambda.GetFunctionInput{FunctionName: name}

	// version 1 uses batch/job:j1 and has an alias.
	_, err := s.lambda.CreateFunction(s.ctx, &lambda.CreateFunctionInput{
		FunctionName: name,
		PackageType:  lambdaTypes.PackageTypeImage,
		Code:         &lambdaTypes.FunctionCode{ImageUri: aws.String(s.uri("batch/job", "j1"))},
		Role:         aws.String("arn:aws:iam::000000000000:role/" + s.prefix + "-lambda"),
		Publish:      true,
	})
	s.must(err)
	s.must(lambda.NewFunctionActiveV2Waiter(s.lambda).Wait(s.ctx, get, time.Minute))
	_, err = s.lambda.CreateAlias(s.ctx, &lambda.CreateAliasInput{
		FunctionName:    name,
		Name:            aws.String("current"),
		FunctionVersion: aws.String("1"),
	})
	s.must(err)

	// version 2 (latest) uses batch/job:j2.
	_, err = s.lambda.UpdateFunctionCode(s.ctx, &lambda.UpdateFunctionCodeInput{
		FunctionName: name,
		ImageUri:     aws.String(s.uri("batch/job", "j2")),
		Publish:      true,
	})
	s.must(err)
	s.must(lambda.NewFunctionUpdatedV2Waiter(s.lambda).Wait(s.ctx, get, time.Minute))
}
