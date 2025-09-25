package ecrm

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

// scanEKSClusters scans EKS clusters and collects images in use by Kubernetes workloads
func (s *Scanner) scanEKSClusters(ctx context.Context, eksConfigs []*EKSClusterConfig) error {
	if len(eksConfigs) == 0 {
		log.Println("[debug] No EKS clusters configured, skipping EKS scan")
		return nil
	}

	clusters, err := eksClusterNames(ctx, s.eks)
	if err != nil {
		return fmt.Errorf("failed to list EKS clusters: %w", err)
	}

	for _, clusterName := range clusters {
		var matchedConfig *EKSClusterConfig
		for _, ec := range eksConfigs {
			if ec.Match(clusterName) {
				matchedConfig = ec
				break
			}
		}
		if matchedConfig == nil {
			continue
		}

		log.Printf("[debug] Scanning EKS cluster %s", clusterName)
		if err := s.scanEKSCluster(ctx, clusterName); err != nil {
			return fmt.Errorf("failed to scan EKS cluster %s: %w", clusterName, err)
		}
	}
	return nil
}

func (s *Scanner) eksKubeClient(ctx context.Context, clusterName string) (*kubernetes.Clientset, error) {
	co, err := s.eks.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &clusterName})

	if err != nil {
		return nil, fmt.Errorf("describe cluster failed: %w", err)
	}
	if co.Cluster == nil || co.Cluster.CertificateAuthority == nil || co.Cluster.CertificateAuthority.Data == nil {
		return nil, fmt.Errorf("cluster %s has no certificate authority data", clusterName)
	}
	caB64 := aws.ToString(co.Cluster.CertificateAuthority.Data)
	caData, err := base64.StdEncoding.DecodeString(caB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cluster CA data: %w", err)
	}
	gen, err := token.NewGenerator(false, false)
	if err != nil {
		return nil, fmt.Errorf("token generator create failed: %w", err)
	}
	tok, err := gen.GetWithOptions(&token.GetTokenOptions{ClusterID: clusterName})
	if err != nil {
		return nil, fmt.Errorf("get token failed: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(&rest.Config{
		Host:            aws.ToString(co.Cluster.Endpoint),
		BearerToken:     tok.Token,
		TLSClientConfig: rest.TLSClientConfig{CAData: caData},
	})
	if err != nil {
		return nil, fmt.Errorf("k8s client create failed: %w", err)
	}
	return clientset, nil
}

func (s *Scanner) scanEKSCluster(ctx context.Context, clusterName string) error {
	clientset, err := s.eksKubeClient(ctx, clusterName)
	if err != nil {
		return err
	}

	if err := s.scanEKSPods(ctx, clientset, clusterName); err != nil {
		return fmt.Errorf("scan pods failed: %w", err)
	}

	if err := s.scanReplicaSets(ctx, clientset, clusterName); err != nil {
		return fmt.Errorf("scan replicasets failed: %w", err)
	}

	if err := s.scanControllerRevisions(ctx, clientset, clusterName); err != nil {
		return fmt.Errorf("scan controllerrevisions failed: %w", err)
	}

	if err := s.scanJobTemplates(ctx, clientset, clusterName); err != nil {
		return fmt.Errorf("scan job templates failed: %w", err)
	}

	return nil
}

func (s *Scanner) scanEKSPods(ctx context.Context, clientset *kubernetes.Clientset, clusterName string) error {
	pods, err := clientset.CoreV1().Pods("").List(ctx, v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list pods failed: %w", err)
	}
	log.Printf("[debug] EKS cluster %s: %d pods", clusterName, len(pods.Items))
	dup := newSet()

	for _, pod := range pods.Items {
		usedBy := fmt.Sprintf("eks:%s/%s/%s", clusterName, pod.Namespace, pod.Name)

		for _, cs := range pod.Status.ContainerStatuses {
			normalized := normalizePodImage(cs.ImageID, cs.Image)
			if normalized == "" {
				continue
			}
			img := ImageURI(normalized)
			if !img.IsECRImage() {
				continue
			}
			if dup.add(normalized) && s.Images.Add(img, usedBy) {
				log.Printf("[info] image %s is used by pod %s/%s (container=%s)", img, pod.Namespace, pod.Name, cs.Name)
			}
		}

		for _, cs := range pod.Status.InitContainerStatuses {
			normalized := normalizePodImage(cs.ImageID, cs.Image)
			if normalized == "" {
				continue
			}
			img := ImageURI(normalized)
			if !img.IsECRImage() {
				continue
			}
			if dup.add(normalized) && s.Images.Add(img, usedBy) {
				log.Printf("[info] image %s is used by pod %s/%s (initContainer=%s)", img, pod.Namespace, pod.Name, cs.Name)
			}
		}
	}

	return nil
}

func (s *Scanner) scanReplicaSets(ctx context.Context, clientset *kubernetes.Clientset, clusterName string) error {
	replicaSets, err := clientset.AppsV1().ReplicaSets("").List(ctx, v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list replicasets failed: %w", err)
	}

	log.Printf("[debug] EKS cluster %s: found %d replicasets", clusterName, len(replicaSets.Items))

	for _, rs := range replicaSets.Items {
		usedBy := fmt.Sprintf("eks:%s/replicaset:%s/%s", clusterName, rs.Namespace, rs.Name)
		s.scanPodTemplateSpec(rs.Spec.Template, usedBy)
	}

	return nil
}

func (s *Scanner) scanControllerRevisions(ctx context.Context, clientset *kubernetes.Clientset, clusterName string) error {
	revisions, err := clientset.AppsV1().ControllerRevisions("").List(ctx, v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list controllerrevisions failed: %w", err)
	}

	log.Printf("[debug] EKS cluster %s: found %d controllerrevisions", clusterName, len(revisions.Items))

	for _, rev := range revisions.Items {
		usedBy := fmt.Sprintf("eks:%s/controllerrevision:%s/%s (revision %d)", clusterName, rev.Namespace, rev.Name, rev.Revision)

		if err := s.extractImagesFromControllerRevision(&rev, usedBy); err != nil {
			log.Printf("[warn] Failed to extract images from ControllerRevision %s/%s: %v", rev.Namespace, rev.Name, err)
			continue
		}
	}

	return nil
}

func (s *Scanner) extractImagesFromControllerRevision(rev *appsv1.ControllerRevision, usedBy string) error {
	if rev.Data.Raw == nil {
		return fmt.Errorf("no data in ControllerRevision")
	}

	var ownerKind string
	for _, owner := range rev.OwnerReferences {
		if owner.Kind == "StatefulSet" || owner.Kind == "DaemonSet" {
			ownerKind = owner.Kind
			break
		}
	}

	if ownerKind == "" {
		return fmt.Errorf("unknown owner type for ControllerRevision")
	}

	decoder := scheme.Codecs.UniversalDeserializer()

	switch ownerKind {
	case "StatefulSet":
		sts := &appsv1.StatefulSet{}
		_, _, err := decoder.Decode(rev.Data.Raw, nil, sts)
		if err != nil {
			return fmt.Errorf("failed to decode StatefulSet data: %w", err)
		}
		s.scanPodTemplateSpec(sts.Spec.Template, usedBy)

	case "DaemonSet":
		ds := &appsv1.DaemonSet{}
		_, _, err := decoder.Decode(rev.Data.Raw, nil, ds)
		if err != nil {
			return fmt.Errorf("failed to decode DaemonSet data: %w", err)
		}
		s.scanPodTemplateSpec(ds.Spec.Template, usedBy)

	default:
		return fmt.Errorf("unsupported owner kind: %s", ownerKind)
	}

	return nil
}

func (s *Scanner) scanJobTemplates(ctx context.Context, clientset *kubernetes.Clientset, clusterName string) error {
	cronJobs, err := clientset.BatchV1().CronJobs("").List(ctx, v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list cronjobs failed: %w", err)
	}

	log.Printf("[debug] EKS cluster %s: found %d cronjobs", clusterName, len(cronJobs.Items))

	for _, cronJob := range cronJobs.Items {
		usedBy := fmt.Sprintf("eks:%s/cronjob:%s/%s", clusterName, cronJob.Namespace, cronJob.Name)
		s.scanPodTemplateSpec(cronJob.Spec.JobTemplate.Spec.Template, usedBy)
	}

	return nil
}

func (s *Scanner) scanPodTemplateSpec(template corev1.PodTemplateSpec, usedBy string) {
	dup := newSet()

	for _, c := range template.Spec.Containers {
		img := ImageURI(c.Image)
		if !img.IsECRImage() {
			continue
		}
		if dup.add(c.Image) && s.Images.Add(img, usedBy) {
			log.Printf("[info] image %s is used by %s (container=%s)", img, usedBy, c.Name)
		}
	}

	for _, c := range template.Spec.InitContainers {
		img := ImageURI(c.Image)
		if !img.IsECRImage() {
			continue
		}
		if dup.add(c.Image) && s.Images.Add(img, usedBy) {
			log.Printf("[info] image %s is used by %s (initContainer=%s)", img, usedBy, c.Name)
		}
	}
}

func normalizePodImage(imageID, imageName string) string {
	if imageID == "" {
		return ""
	}
	cleanedID := strings.TrimPrefix(imageID, "docker-pullable://")
	if strings.Contains(cleanedID, "@sha256:") {
		return cleanedID
	}
	if strings.HasPrefix(cleanedID, "sha256:") && imageName != "" {
		digest := cleanedID
		baseImage := strings.Split(imageName, ":")[0]
		return baseImage + "@" + digest
	}
	return ""
}
