package ecrm

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/alecthomas/kong"
	"github.com/fatih/color"
	"github.com/fujiwara/logutils"
)

func init() {
	// keep backward compatibility with ecrm 0.4.0
	if c, ok := os.LookupEnv("ECRM_NO_COLOR"); !ok {
		return
	} else {
		if noColor, err := strconv.ParseBool(c); err != nil {
			panic("ECRM_NO_COLOR must be bool value: " + err.Error())
		} else {
			os.Setenv("ECRM_COLOR", strconv.FormatBool(!noColor))
		}
	}
}

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

/*
NAME:
   ecrm - A command line tool for managing ECR repositories

USAGE:
   ecrm [global options] command [command options] [arguments...]

VERSION:
   current

COMMANDS:
   delete    Scan ECS/Lambda resources and delete unused ECR images.
   generate  Generate ecrm.yaml
   plan      Scan ECS/Lambda resources and find unused ECR images to delete safety.
   help, h   Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --config FILE, -c FILE  Load configuration from FILE (default: "ecrm.yaml") [$ECRM_CONFIG]
   --format value          plan output format (table, json) (default: "table") [$ECRM_FORMAT]
   --log-level value       Set log level (debug, info, notice, warn, error) (default: "info") [$ECRM_LOG_LEVEL]
   --no-color              Whether or not to color the output (default: false) [$ECRM_NO_COLOR]
   --help, -h              show help (default: false)
   --version, -v           print the version (default: false)
*/

type CLI struct {
	Config   string `help:"Load configuration from FILE" short:"c" default:"ecrm.yaml" env:"ECRM_CONFIG"`
	LogLevel string `help:"Set log level (debug, info, notice, warn, error)" default:"info" env:"ECRM_LOG_LEVEL"`
	Format   string `help:"plan output format (table, json)" default:"table" enum:"table,json" env:"ECRM_FORMAT"`
	Color    bool   `help:"Whether or not to color the output" default:"true" env:"ECRM_COLOR" negatable:""`
	Version  bool   `help:"Show version"`

	Plan     *PlanCLI     `cmd:"" help:"Scan ECS/Lambda resources and find unused ECR images to delete safety."`
	Generate *GenerateCLI `cmd:"" help:"Generate ecrm.yaml"`
	Delete   *DeleteCLI   `cmd:"" help:"Scan ECS/Lambda resources and delete unused ECR images."`

	command string
	app     *App
}

type PlanCLI struct {
	Repository string `short:"r" help:"plan for only images in REPOSITORY" env:"ECRM_REPOSITORY"`
}

type GenerateCLI struct {
}

type DeleteCLI struct {
	Force      bool   `help:"force delete images without confirmation" env:"ECRM_FORCE"`
	Repository string `short:"r" help:"delete only images in REPOSITORY" env:"ECRM_REPOSITORY"`
}

func (app *App) NewCLI() *CLI {
	c := &CLI{}
	k := kong.Parse(c)
	c.command = k.Command()
	c.app = app
	return c
}

func (c *CLI) RunContext(ctx context.Context) error {
	if c.Version {
		log.Println(c.app.Version)
		return nil
	}
	color.NoColor = !c.Color
	SetLogLevel(c.LogLevel)

	switch c.command {
	case "plan":
		return c.app.Run(ctx, c.Config, Option{
			Delete:     false,
			Repository: c.Plan.Repository,
			NoColor:    !c.Color,
			Format:     newOutputFormatFrom(c.Format),
		})
	case "generate":
		return c.app.GenerateConfig(ctx, c.Config, Option{
			NoColor: !c.Color,
		})
	case "delete":
		return c.app.Run(ctx, c.Config, Option{
			Delete:     true,
			Force:      c.Delete.Force,
			Repository: c.Delete.Repository,
			NoColor:    !c.Color,
			Format:     newOutputFormatFrom(c.Format),
		})
	}

	return nil
}

func (app *App) NewLambdaAction() func() error {
	return func() error {
		return nil
		/*
			subcommand := os.Getenv("ECRM_COMMAND")
			lambda.Start(func() error {
				format := newOutputFormatFrom(c.String("format"))
				return app.Run(
					c.Context,
					c.String("config"),
					Option{
						Delete:     subcommand == "delete",
						Force:      subcommand == "delete", //If it works as bootstrap for a Lambda function, delete images without confirmation.
						Repository: os.Getenv("ECRM_REPOSITORY"),
						NoColor:    c.Bool("no-color"),
						Format:     format,
					},
				)
			})
			return nil
		*/
	}
}
