# Changelog

## [v0.7.0](https://github.com/fujiwara/ecrm/compare/v0.6.1...v0.7.0) - 2025-10-02
- Update Go toolchain to go1.25.1 by @github-actions[bot] in https://github.com/fujiwara/ecrm/pull/92
- ecrm plan --scanned-files by @fujiwara in https://github.com/fujiwara/ecrm/pull/95
- Add external command support for custom image discovery by @fujiwara in https://github.com/fujiwara/ecrm/pull/96
- Add args inputs for action.yml by @fujiwara in https://github.com/fujiwara/ecrm/pull/98
- fix actions test by @fujiwara in https://github.com/fujiwara/ecrm/pull/99

## [v0.6.1](https://github.com/fujiwara/ecrm/compare/v0.6.0...v0.6.1) - 2025-09-19
- Add installation guide to README.md by @ebi-yade in https://github.com/fujiwara/ecrm/pull/61
- update toolchain workflow by @fujiwara in https://github.com/fujiwara/ecrm/pull/74
- Update Go toolchain to go1.24.3 by @github-actions[bot] in https://github.com/fujiwara/ecrm/pull/75
- Update Go toolchain to go1.24.4 by @github-actions[bot] in https://github.com/fujiwara/ecrm/pull/81
- Add setup script for GitHub Actions by @kei-s16 in https://github.com/fujiwara/ecrm/pull/82
- Update Go toolchain to go1.24.5 by @github-actions[bot] in https://github.com/fujiwara/ecrm/pull/86
- Update Go toolchain to go1.24.6 by @github-actions[bot] in https://github.com/fujiwara/ecrm/pull/88
- Update Go toolchain to go1.24.7 by @github-actions[bot] in https://github.com/fujiwara/ecrm/pull/90
- Immutable release by @fujiwara in https://github.com/fujiwara/ecrm/pull/91

## [v0.6.0](https://github.com/fujiwara/ecrm/compare/v0.5.0...v0.6.0) - 2024-11-01
- Bump github.com/alecthomas/kong from 0.9.0 to 1.3.0 by @dependabot in https://github.com/fujiwara/ecrm/pull/56
- Bump github.com/goccy/go-yaml from 1.9.4 to 1.13.2 by @dependabot in https://github.com/fujiwara/ecrm/pull/55
- Bump github.com/google/go-containerregistry from 0.20.0 to 0.20.2 by @dependabot in https://github.com/fujiwara/ecrm/pull/52
- Bump github.com/dustin/go-humanize from 1.0.0 to 1.0.1 by @dependabot in https://github.com/fujiwara/ecrm/pull/51
- bump Go 1.23 by @fujiwara in https://github.com/fujiwara/ecrm/pull/59
- Bump the aws-sdk-go-v2 group across 1 directory with 5 updates by @dependabot in https://github.com/fujiwara/ecrm/pull/57
- obsolete "keep_aliase". All aliased versions are always kept. by @fujiwara in https://github.com/fujiwara/ecrm/pull/58

## [v0.5.0](https://github.com/fujiwara/ecrm/compare/v0.4.0...v0.5.0) - 2024-09-13
- modernization by @fujiwara in https://github.com/fujiwara/ecrm/pull/21
- Bump github.com/Songmu/prompter from 0.5.0 to 0.5.1 by @dependabot in https://github.com/fujiwara/ecrm/pull/29
- Bump github.com/google/go-containerregistry from 0.16.1 to 0.20.0 by @dependabot in https://github.com/fujiwara/ecrm/pull/28
- Bump the aws-sdk-go-v2 group with 5 updates by @dependabot in https://github.com/fujiwara/ecrm/pull/25
- Bump github.com/aws/aws-lambda-go from 1.32.0 to 1.47.0 by @dependabot in https://github.com/fujiwara/ecrm/pull/27
- Bump github.com/fatih/color from 1.13.0 to 1.17.0 by @dependabot in https://github.com/fujiwara/ecrm/pull/26
- Bump goreleaser/goreleaser-action from 2 to 6 by @dependabot in https://github.com/fujiwara/ecrm/pull/24
- Bump actions/checkout from 3 to 4 by @dependabot in https://github.com/fujiwara/ecrm/pull/23
- Hold images identified by digest. by @fujiwara in https://github.com/fujiwara/ecrm/pull/30
- Fix hold order by @fujiwara in https://github.com/fujiwara/ecrm/pull/40
- feat: Support manifest list as Image Index of "Docker Image Manifest V2 Schema 2" by @yamoyamoto in https://github.com/fujiwara/ecrm/pull/42
- Switch to kong by @fujiwara in https://github.com/fujiwara/ecrm/pull/41
- Add scan command. by @fujiwara in https://github.com/fujiwara/ecrm/pull/44
- Refactor types by @fujiwara in https://github.com/fujiwara/ecrm/pull/45
- collect ECS tasks whose desired status is STOPPED. by @fujiwara in https://github.com/fujiwara/ecrm/pull/46
- Bump github.com/samber/lo from 1.26.0 to 1.47.0 by @dependabot in https://github.com/fujiwara/ecrm/pull/38
- Bump github.com/fujiwara/logutils from 1.1.0 to 1.1.2 by @dependabot in https://github.com/fujiwara/ecrm/pull/35
- Bump github.com/k1LoW/duration from 1.1.0 to 1.2.0 by @dependabot in https://github.com/fujiwara/ecrm/pull/33

## [v0.4.0](https://github.com/fujiwara/ecrm/compare/v0.3.3...v0.4.0) - 2023-08-29
- support to expire image indexes and soci indexes. by @fujiwara in https://github.com/fujiwara/ecrm/pull/17
- use go 1.21 by @fujiwara in https://github.com/fujiwara/ecrm/pull/19

## [v0.3.3](https://github.com/fujiwara/ecrm/compare/v0.3.2...v0.3.3) - 2022-09-06
- Move -format flag to global. by @fujiwara in https://github.com/fujiwara/ecrm/pull/14

## [v0.3.2](https://github.com/fujiwara/ecrm/compare/v0.3.1...v0.3.2) - 2022-09-06
- need output format for delete command by @mashiike in https://github.com/fujiwara/ecrm/pull/11

## [v0.3.1](https://github.com/fujiwara/ecrm/compare/v0.3.0...v0.3.1) - 2022-08-31
- add ECRM_FORMAT env by @fujiwara in https://github.com/fujiwara/ecrm/pull/10

## [v0.3.0](https://github.com/fujiwara/ecrm/compare/v0.2.0...v0.3.0) - 2022-08-31
- feat: add JSON output format by @aereal in https://github.com/fujiwara/ecrm/pull/8
- Fix cli default argument value. by @fujiwara in https://github.com/fujiwara/ecrm/pull/9

## [v0.2.0](https://github.com/fujiwara/ecrm/compare/v0.1.2...v0.2.0) - 2022-08-17
- move cli codes to cli.go by @fujiwara in https://github.com/fujiwara/ecrm/pull/4
- pass ctx by @fujiwara in https://github.com/fujiwara/ecrm/pull/5
- add generate command by @fujiwara in https://github.com/fujiwara/ecrm/pull/6

## [v0.1.2](https://github.com/fujiwara/ecrm/compare/v0.1.1...v0.1.2) - 2022-07-29
- Add --no-color option by @mashiike in https://github.com/fujiwara/ecrm/pull/2
- BatchDeleteItem's 'imageIds' must be less than or equal to 100 in length, so by @mashiike in https://github.com/fujiwara/ecrm/pull/3

## [v0.1.1](https://github.com/fujiwara/ecrm/compare/v0.1.0...v0.1.1) - 2022-06-21
- Feature/run as lambda bootstrap by @mashiike in https://github.com/fujiwara/ecrm/pull/1

## [v0.1.0](https://github.com/fujiwara/ecrm/compare/v0.0.0...v0.1.0) - 2021-10-25

## [v0.0.0](https://github.com/fujiwara/ecrm/commits/v0.0.0) - 2021-10-18
