package main

import (
	"context"
	"log"
	"os"
	"sort"

	"github.com/fujiwara/ecrm"
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
				Usage: "scan ECR repositories",
				Action: func(c *cli.Context) error {
					return ecrmApp.Run(
						c.String("config"),
						ecrm.Option{Delete: false},
					)
				},
			},
			{
				Name:  "delete",
				Usage: "delete images on ECR",
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

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
