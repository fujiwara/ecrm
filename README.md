# ecrm

A command line tool for managing Amazon ECR repositories.

ecrm can delete "unused" images safety.

"unused" means,

- Images are not used by running tasks in ECS clusters.
- Images are not specified in available ECS service deployments.
- Images are not specified in existing ECS task definitions (latest N revisions).
- Images are not specified by Lambda functions (latest N versions).

## Install

### Homebrew (macOS and Linux)

```console
$ brew install fujiwara/tap/ecrm
```

### aqua

[aqua](https://aquaproj.github.io/) is a declarative CLI Version Manager.

```console
$ aqua g -i fujiwara/ecrm
```

### Binary packages

[Releases](https://github.com/fujiwara/ecrm/releases)

### GitHub Actions

Action fujiwara/ecrm@main installs ecrm binary for Linux.

This action installs the specified version of ecrm.

```yml
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: fujiwara/ecrm@main
        with:
          version: v0.7.0
      - run: |
          ecrm delete --force
```

When the args input is specified, the command `ecrm {args}` is executed after the installation.

```yaml
      - uses: fujiwara/ecrm@main
        with:
          version: v0.7.0
          args: delete --force
```

Note:
- `version` is not required, but it is recommended that the version be specified.
  - The default version is not fixed and may change in the future.
- `os` and `arch` are automatically detected.


## Usage

```
Usage: ecrm <command> [flags]

Flags:
  -h, --help                  Show context-sensitive help.
  -c, --config="ecrm.yaml"    Load configuration from FILE ($ECRM_CONFIG)
      --log-level="info"      Set log level (debug, info, notice, warn, error)
                              ($ECRM_LOG_LEVEL)
      --[no-]color            Whether or not to color the output ($ECRM_COLOR)
      --version               Show version.

Commands:
  generate [flags]
    Generate a configuration file.

  scan [flags]
    Scan ECS/Lambda resources. Output image URIs in use.

  plan [flags]
    Scan ECS/Lambda resources and find unused ECR images that can be deleted
    safely.

  delete [flags]
    Scan ECS/Lambda resources and delete unused ECR images.

  version [flags]
    Show version.
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
external_commands:
  - command: ["path/to/command", "arg1", "arg2"]
    timeout: 30s
    env:
      AWS_REGION: "us-west-2"
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

### scan command

`ecrm scan` scans your AWS account's ECS, Lambda, and ECR resources. It outputs image URIs in use.

`ecrm scan --output path/to/file` writes the image URIs in use to the file as JSON format.

The scanned files can be used in the next `ecrm delete` command with `--scanned-files` option.

The format of the file is a simple JSON array of image URIs.

```json
[
  "012345678901.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:latest",
  "012345678901.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar@sha256:abcdef1234567890..."
]
```

You can create scanned files manually as you need.

If your workload runs on platforms that ecrm does not support (for example, AWS AppRunner, Amazon EKS, etc.), you can use ecrm with the scanned file you created.

### plan command

The plan command runs `ecrm scan` internally and then creates a plan to delete images.

`ecrm plan` shows summaries of images in ECR repositories that can be deleted safely.

```console
Usage: ecrm plan [flags]

Scan ECS/Lambda resources and find unused ECR images to delete safety.

Flags:
  -o, --output="-"                         File name of the output. The default is STDOUT ($ECRM_OUTPUT).
      --format="table"                     Output format of plan(table, json) ($ECRM_FORMAT)
      --[no-]scan                          Scan ECS/Lambda resources that in use ($ECRM_SCAN).
  -r, --repository=STRING                  Manage images in the repository only ($ECRM_REPOSITORY).
      --scanned-files=SCANNED-FILES,...    Files of the scan result. ecrm does not delete images in these files
                                           ($ECRM_SCANNED_FILES).
```

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

The delete command first runs `ecrm scan`, then creates a plan to delete images, and finally deletes them.

By default, `ecrm delete` shows a prompt before deleting images. You can use `--force` option to delete images without confirmation.

```console
Usage: ecrm delete [flags]

Scan ECS/Lambda resources and delete unused ECR images.

Flags:
  -o, --output="-"                         File name of the output. The default is STDOUT ($ECRM_OUTPUT).
      --format="table"                     Output format of plan(table, json) ($ECRM_FORMAT)
      --[no-]scan                          Scan ECS/Lambda resources that in use ($ECRM_SCAN).
  -r, --repository=STRING                  Manage images in the repository only ($ECRM_REPOSITORY).
      --scanned-files=SCANNED-FILES,...    Files of the scan result. ecrm does not delete images in these
                                           files ($ECRM_SCANNED_FILES).
      --force                              force delete images without confirmation ($ECRM_FORCE)
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

### External Commands

`ecrm` allows you to run external commands during the scan and delete process.

You can use external commands to integrate with other systems or platforms that `ecrm` does not natively support.

For example, if you have workloads running on AWS AppRunner or Amazon EKS or etc., you can create a script that fetches the image URIs used by those services and outputs them in the required JSON format.

```yaml
external_commands:
  - command: ["path/to/command", "arg1", "arg2"]
    timeout: 30s
    env:
      AWS_REGION: "us-west-2"
    dir: "/path/to/working/directory"
```

The command will output a JSON array of image URIs to STDOUT. `ecrm` will read the output and include the image URIs in its scan results to avoid deleting them.

The format of the output required is a simple JSON array of image URIs.

```json
[
  "012345678901.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar:latest",
  "012345678901.dkr.ecr.ap-northeast-1.amazonaws.com/foo/bar@sha256:abcdef1234567890..."
]
```

#### Use-case of External Commands


### Multi accounts / regions support.

`ecrm` supports a single AWS account and region for each run.

If your workloads are deployed in multiple regions or accounts, you should run `ecrm scan` for each region or account to collect all image URIs in use.

Then, you can run `ecrm delete` with the `--scanned-files` option to delete unused images in all regions or accounts.

For example, your ECR in the `account-a`, and your ECS clusters are deployed in `account-a` and `account-b`.

At first, you run `ecrm scan` for each account.

```console
$ AWS_PROFILE=account-a ecrm scan --output scan-account-a.json
$ AWS_PROFILE=account-b ecrm scan --output scan-account-b.json
```

Now, you can run `ecrm delete` with the `--scanned-files` option to safely delete unused images in all accounts.

```console
$ AWS_PROFILE=account-a ecrm delete --scanned-files scan-account-a.json,scan-account-b.json
```

## Author

Copyright (c) 2021 FUJIWARA Shunichiro

## LICENSE

MIT
