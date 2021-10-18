# ecrm

A command line tool for managing ECR repositories.

ecrm can delete "unused" images safety.

"unused" means,

- Images not specified in running tasks in ECS clusters.
- Images not specified in avaliable ECS service deployments.

## Usage

```
NAME:
   ecrm - A command line tool for managing ECR repositories

USAGE:
   ecrm [global options] command [command options] [arguments...]

COMMANDS:
   delete   delete images on ECR
   scan     scan ECR repositories
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --config FILE, -c FILE  Load configuration from FILE (default: ecrm.yaml) [$ECRM_CONFIG]
   --help, -h              show help (default: false)
```

## Configurations

Configuration file is YAML format.

```yaml
clusters:
  - name: my-cluster
  - name_pattern: "prod*"
repositories:
  - name_pattern: "prod/*"
    expires: 90days
    keep_tag_patterns:
      - latest
  - name_pattern: "dev/*"
    expires: 30days
```

### scan command

```console
$ ecrm scan --help
NAME:
   ecrm scan - scan ECS clusters and find unused ECR images to delete safety.

USAGE:
   ecrm scan [command options] [arguments...]

OPTIONS:
   --help, -h  show help (default: false)
```

### delete command

```console
$ ecrm delete --help
NAME:
   ecrm delete - scan ECS clusters and delete unused ECR images.

USAGE:
   ecrm delete [command options] [arguments...]

OPTIONS:
   --force     force delete images without confirmation (default: false) [$ECRM_FORCE]
   --help, -h  show help (default: false)
```

## Author

Copyright (c) 2021 FUJIWARA Shunichiro
## LICENSE

MIT
