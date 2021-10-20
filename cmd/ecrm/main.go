package main

import (
	"context"
	"log"
	"os"
	"sort"

	"github.com/fatih/color"
	"github.com/fujiwara/ecrm"
	"github.com/fujiwara/logutils"
	"github.com/urfave/cli/v2"
)

func main() {
	ecrmApp, err := ecrm.New(context.Background(), os.Getenv("AWS_REGION"))
	if err != nil {
		log.Fatal(err)
	}
	app := &cli.App{
		Name:  "ecrm",
		Usage: "A command line tool for managing ECR repositories",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				DefaultText: "ecrm.yaml",
				Usage:       "Load configuration from `FILE`",
				EnvVars:     []string{"ECRM_CONFIG"},
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "scan",
				Usage: "scan ECS clusters and find unused ECR images to delete safety.",
				Action: func(c *cli.Context) error {
					return ecrmApp.Run(
						c.String("config"),
						ecrm.Option{Delete: false},
					)
				},
			},
			{
				Name:  "delete",
				Usage: "scan ECS clusters and delete unused ECR images.",
				Action: func(c *cli.Context) error {
					return ecrmApp.Run(
						c.String("config"),
						ecrm.Option{
							Delete: true,
							Force:  c.Bool("force"),
						},
					)
				},
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "force",
						Usage:   "force delete images without confirmation",
						EnvVars: []string{"ECRM_FORCE"},
					},
				},
			},
		},
	}

	sort.Sort(cli.FlagsByName(app.Flags))
	sort.Sort(cli.CommandsByName(app.Commands))

	var filter = &logutils.LevelFilter{
		Levels: []logutils.LogLevel{"debug", "info", "notice", "warn", "error"},
		ModifierFuncs: []logutils.ModifierFunc{
			nil,
			logutils.Color(color.FgWhite),
			logutils.Color(color.FgHiBlue),
			logutils.Color(color.FgYellow),
			logutils.Color(color.FgRed, color.Bold),
		},
		MinLevel: logutils.LogLevel("debug"),
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
