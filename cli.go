package ecrm

import (
	"log"
	"os"
	"sort"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/fatih/color"
	"github.com/fujiwara/logutils"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
)

var LogLevelFilter = &logutils.LevelFilter{
	Levels: []logutils.LogLevel{"debug", "info", "notice", "warn", "error"},
	ModifierFuncs: []logutils.ModifierFunc{
		nil,
		logutils.Color(color.FgWhite),
		logutils.Color(color.FgHiBlue),
		logutils.Color(color.FgYellow),
		logutils.Color(color.FgRed, color.Bold),
	},
	Writer: os.Stderr,
}

func SetLogLevel(level string) {
	if level != "" {
		LogLevelFilter.MinLevel = logutils.LogLevel(level)
	}
	log.SetOutput(LogLevelFilter)
	log.Println("[debug] Setting log level to", level)
}

func (app *App) NewPlanCommand() *cli.Command {
	return &cli.Command{
		Name:  "plan",
		Usage: "Scan ECS/Lambda resources and find unused ECR images to delete safety.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repository",
				Aliases: []string{"r"},
				Usage:   "plan for only images in `REPOSITORY`",
				EnvVars: []string{"ECRM_REPOSITORY"},
			},
			&cli.StringFlag{
				Name:    "format",
				Value:   "table",
				Usage:   "plan output format (table, json)",
				EnvVars: []string{"ECRM_FORMAT"},
			},
		},
		Action: func(c *cli.Context) error {
			format, err := newOutputFormatFrom(c.String("format"))
			if err != nil {
				return err
			}
			return app.Run(
				c.Context,
				c.String("config"),
				Option{
					Repository: c.String("repository"),
					NoColor:    c.Bool("no-color"),
					Format:     format,
				},
			)
		},
	}
}

func (app *App) NewDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Scan ECS/Lambda resources and delete unused ECR images.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Usage:   "force delete images without confirmation",
				EnvVars: []string{"ECRM_FORCE"},
			},
			&cli.StringFlag{
				Name:    "repository",
				Aliases: []string{"r"},
				Usage:   "delete only images in `REPOSITORY`",
				EnvVars: []string{"ECRM_REPOSITORY"},
			},
		},
		Action: func(c *cli.Context) error {
			return app.Run(
				c.Context,
				c.String("config"),
				Option{
					Delete:     true,
					Force:      c.Bool("force"),
					Repository: c.String("repository"),
					NoColor:    c.Bool("no-color"),
				},
			)
		},
	}
}

func (app *App) NewCLI() *cli.App {
	cliApp := &cli.App{
		Name:  "ecrm",
		Usage: "A command line tool for managing ECR repositories",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Value:   "ecrm.yaml",
				Usage:   "Load configuration from `FILE`",
				EnvVars: []string{"ECRM_CONFIG"},
			},
			&cli.StringFlag{
				Name:    "log-level",
				Value:   "info",
				Usage:   "Set log level (debug, info, notice, warn, error)",
				EnvVars: []string{"ECRM_LOG_LEVEL"},
			},
			&cli.BoolFlag{
				Name:    "no-color",
				Value:   !isatty.IsTerminal(os.Stdout.Fd()),
				Usage:   "Whether or not to color the output",
				EnvVars: []string{"ECRM_NO_COLOR"},
			},
		},
		Before: func(c *cli.Context) error {
			color.NoColor = c.Bool("no-color")
			SetLogLevel(c.String("log-level"))
			return nil
		},
		Commands: []*cli.Command{
			app.NewPlanCommand(),
			app.NewDeleteCommand(),
			app.NewGenerateCommand(),
		},
	}
	sort.Sort(cli.FlagsByName(cliApp.Flags))
	sort.Sort(cli.CommandsByName(cliApp.Commands))
	return cliApp
}

func (app *App) NewLambdaAction() func(c *cli.Context) error {
	return func(c *cli.Context) error {
		subcommand := os.Getenv("ECRM_COMMAND")
		lambda.Start(func() error {
			return app.Run(
				c.Context,
				c.String("config"),
				Option{
					Delete:     subcommand == "delete",
					Force:      subcommand == "delete", //If it works as bootstrap for a Lambda function, delete images without confirmation.
					Repository: os.Getenv("ECRM_REPOSITORY"),
					NoColor:    c.Bool("no-color"),
				},
			)
		})
		return nil
	}
}
