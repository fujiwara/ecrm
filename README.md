# ecrm

A command line tool for managing ECR repositories.

ecrm can delete "unused" images safety.

"unused" means,

- Images not specified in running tasks in ECS clusters.
- Images not specified in available ECS service deployments.
- Images not specified in exists ECS task definitions.
- Images not specified in using Lambda functions (PackageType=Image).

## Usage

```
Usage: ecrm <command> [flags]

Flags:
  -h, --help                  Show context-sensitive help.
  -c, --config="ecrm.yaml"    Load configuration from FILE ($ECRM_CONFIG)
      --log-level="info"      Set log level (debug, info, notice, warn, error) ($ECRM_LOG_LEVEL)
      --format="table"        plan output format (table, json) ($ECRM_FORMAT)
      --[no-]color            Whether or not to color the output ($ECRM_COLOR)
      --version               Show version

Commands:
  plan [flags]
    Scan ECS/Lambda resources and find unused ECR images to delete safety.

  generate [flags]
    Generate ecrm.yaml

  delete [flags]
    Scan ECS/Lambda resources and delete unused ECR images.
```

## Configurations

Configuration file is YAML format. `ecrm generate` can generate a configuration file.

```yaml
clusters:
  - name: my-cluster
  - name_pattern: "prod*"
  - name_pattern: "dev*"
task_definitions:
  - name: "*"
    keep_count: 3
lambda_functions:
  - name: "*"
    keep_count: 3
repositories:
  - name_pattern: "prod/*"
    expires: 90days
    keep_tag_patterns:
      - latest
  - name_pattern: "dev/*"
    expires: 30days
```

### generate command

`ecrm generate` scans ECS, Lambda and ECR resources in an AWS account and generates a configuration file.

```
Usage: ecrm generate [flags]

Generate ecrm.yaml
```

### plan command

```console
Usage: ecrm plan [flags]

Scan ECS/Lambda resources and find unused ECR images to delete safety.

Flags:
  -r, --repository=STRING     plan for only images in REPOSITORY ($ECRM_REPOSITORY)
```

`ecrm plan` shows summaries of unused images in ECR.

`ecrm delete` deletes these images (in `EXPIRED` columns) actually.

```console
$ ecrm plan
       REPOSITORY      |    TOTAL     |    EXPIRED    |    KEEP      
-----------------------+--------------+---------------+--------------
      dev/app          | 732 (594 GB) | -707 (574 GB) | 25 (21 GB)   
      dev/nginx        | 720 (28 GB)  | -697 (27 GB)  | 23 (875 MB)  
      prod/app         | 97 (80 GB)   | -87 (72 GB)   | 10 (8.4 GB)  
      prod/nginx       | 95 (3.7 GB)  | -85 (3.3 GB)  | 10 (381 MB)  
```

### delete command

```console
Usage: ecrm delete [flags]

Scan ECS/Lambda resources and delete unused ECR images.

Flags:
      --force                 force delete images without confirmation ($ECRM_FORCE)
  -r, --repository=STRING     delete only images in REPOSITORY ($ECRM_REPOSITORY)
```

## Notes

### Support to image indexes and soci indexes.

ecrm supports image indexes and soci (Seekable OCI) indexes. ecrm deletes these images that are related to expired images safely.

1. Scans ECR repositories.
   - Detect image type (Image, Image index, Soci index).
2. Find expired images.
3. Find expired image indexes related to expired images by the image tag (sha256-{digest of image}).
4. Find soci indexes related to expired image indexes using ECR BatchGetImage API for expired images.

An example output is here.

```
  REPOSITORY |    TYPE     |   TOTAL    |   EXPIRED   |    KEEP     
-------------+-------------+------------+-------------+-------------
  xxx/app    | Image       | 30 (40 GB) | -27 (36 GB) | 3 (3.8 GB)  
  xxx/app    | Image index | 5 (163 MB) | -3 (98 MB)  | 2 (65 MB)   
  xxx/app    | Soci index  | 5 (163 MB) | -3 (98 MB)  | 2 (65 MB)  
```

See also
- [Under the hood: Lazy Loading Container Images with Seekable OCI and AWS Fargate](https://aws.amazon.com/jp/blogs/containers/under-the-hood-lazy-loading-container-images-with-seekable-oci-and-aws-fargate/)
- [AWS Fargate Enables Faster Container Startup using Seekable OCI](https://aws.amazon.com/jp/blogs/aws/aws-fargate-enables-faster-container-startup-using-seekable-oci/)

## Author

Copyright (c) 2021 FUJIWARA Shunichiro

## LICENSE

MIT
