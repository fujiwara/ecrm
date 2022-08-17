# ecrm

A command line tool for managing ECR repositories.

ecrm can delete "unused" images safety.

"unused" means,

- Images not specified in running tasks in ECS clusters.
- Images not specified in avaliable ECS service deployments.
- Images not specified in exists ECS task definitions.
- Images not specified in using Lambda functions (PackageType=Image).

## Usage

```
NAME:
   ecrm - A command line tool for managing ECR repositories

USAGE:
   ecrm [global options] command [command options] [arguments...]

COMMANDS:
   delete    Scan ECS/Lambda resources and delete unused ECR images.
   generate  Genarete ecrm.yaml
   plan      Scan ECS/Lambda resources and find unused ECR images to delete safety.
   help, h   Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --config FILE, -c FILE  Load configuration from FILE (default: "ecrm.yaml") [$ECRM_CONFIG]
   --log-level value       Set log level (debug, info, notice, warn, error) (default: "info") [$ECRM_LOG_LEVEL]
   --no-color              Whether or not to color the output (default: false) [$ECRM_NO_COLOR]
   --help, -h              show help (default: false)
```

## Configurations

Configuration file is YAML format. `ecrm generate` can generate a configuration file.

```yaml
clusters:
  - name: my-cluster
  - name_pattern: "prod*"
task_definitions:
  - name: "*"
    keep_count: 3
lambda_funcions:
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

`ecrm generate` scans ECS, Lambda and ECR resources in an AWS account and generate a configuration file.

```console
$ ecrm generate --help
NAME:
   ecrm generate - Genarete ecrm.yaml

USAGE:
   ecrm generate [command options] [arguments...]

OPTIONS:
   --help, -h  show help (default: false)
```

### plan command

```console
$ ecrm plan --help
NAME:
   ecrm plan - scan ECS resources and find unused ECR images to delete safety.

USAGE:
   ecrm plan [command options] [arguments...]

OPTIONS:
   --repository REPOSITORY, -r REPOSITORY  plan for only images in REPOSITORY [$ECRM_REPOSITORY]
   --help, -h                              show help (default: false)
```

### delete command

```console
$ ecrm delete --help
NAME:
   ecrm delete - scan ECS resources and delete unused ECR images.

USAGE:
   ecrm delete [command options] [arguments...]

OPTIONS:
   --force                                 force delete images without confirmation (default: false) [$ECRM_FORCE]
   --repository REPOSITORY, -r REPOSITORY  delete only images in REPOSITORY [$ECRM_REPOSITORY]
   --help, -h                              show help (default: false)
```

## Author

Copyright (c) 2021 FUJIWARA Shunichiro

## LICENSE

MIT
