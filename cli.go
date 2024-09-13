package ecrm

import (
	"context"
	"fmt"
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

type CLI struct {
	Config      string `help:"Load configuration from FILE" short:"c" default:"ecrm.yaml" env:"ECRM_CONFIG"`
	LogLevel    string `help:"Set log level (debug, info, notice, warn, error)" default:"info" env:"ECRM_LOG_LEVEL"`
	Color       bool   `help:"Whether or not to color the output" default:"true" env:"ECRM_COLOR" negatable:""`
	ShowVersion bool   `help:"Show version." name:"version"`

	Generate *GenerateCLI `cmd:"" help:"Generate a configuration file."`
	Scan     *ScanCLI     `cmd:"" help:"Scan ECS/Lambda resources. Output image URIs in use."`
	Plan     *PlanCLI     `cmd:"" help:"Scan ECS/Lambda resources and find unused ECR images that can be deleted safely."`
	Delete   *DeleteCLI   `cmd:"" help:"Scan ECS/Lambda resources and delete unused ECR images."`
	Version  struct{}     `cmd:"" default:"1" help:"Show version."`

	command string
	app     *App
}

type GenerateCLI struct {
}

func (c *GenerateCLI) Option() *Option {
	return &Option{}
}

type PlanCLI struct {
	PlanOrDelete
}

func (c *PlanCLI) Option() *Option {
	return &Option{
		OutputFile:   c.Output,
		Format:       newOutputFormatFrom(c.Format),
		Scan:         c.Scan,
		ScannedFiles: c.ScannedFiles,
		Delete:       false,
		Repository:   RepositoryName(c.Repository),
	}
}

type DeleteCLI struct {
	PlanOrDelete
	Force bool `help:"force delete images without confirmation" env:"ECRM_FORCE"`
}

func (c *DeleteCLI) Option() *Option {
	return &Option{
		OutputFile:   c.Output,
		Format:       newOutputFormatFrom(c.Format),
		Scan:         c.Scan,
		ScannedFiles: c.ScannedFiles,
		Delete:       true,
		Force:        c.Force,
		Repository:   RepositoryName(c.Repository),
	}
}

type PlanOrDelete struct {
	OutputCLI
	Format       string   `help:"Output format of plan(table, json)" default:"table" enum:"table,json" env:"ECRM_FORMAT"`
	Scan         bool     `help:"Scan ECS/Lambda resources that in use." default:"true" negatable:"" env:"ECRM_SCAN"`
	ScannedFiles []string `help:"Files of the scan result. ecrm does not delete images in these files." env:"ECRM_SCANNED_FILES"`
	Repository   string   `help:"Delete only images in the repository." short:"r" env:"ECRM_REPOSITORY"`
}

type OutputCLI struct {
	Output string `help:"File name of the output. The default is STDOUT." short:"o" default:"-" env:"ECRM_OUTPUT"`
}

type ScanCLI struct {
	OutputCLI
}

func (c *ScanCLI) Option() *Option {
	return &Option{
		OutputFile: c.Output,
		Scan:       true,
		ScanOnly:   true,
	}
}

func (app *App) NewCLI() *CLI {
	c := &CLI{}
	k := kong.Parse(c)
	c.command = k.Command()
	c.app = app
	return c
}

func (c *CLI) Run(ctx context.Context) error {
	color.NoColor = !c.Color
	SetLogLevel(c.LogLevel)
	log.Println("[debug] region:", c.app.region)

	switch c.command {
	case "generate":
		return c.app.GenerateConfig(ctx, c.Config)
	case "scan":
		return c.app.Run(ctx, c.Config, c.Scan.Option())
	case "plan":
		return c.app.Run(ctx, c.Config, c.Plan.Option())
	case "delete":
		return c.app.Run(ctx, c.Config, c.Delete.Option())
	case "version":
		fmt.Printf("ecrm version %s\n", c.app.Version)
		if !c.ShowVersion {
			fmt.Println("Run with --help for usage.")
		}
		return nil
	default:
		return fmt.Errorf("unknown command: %s", c.command)
	}
}

func (c *CLI) NewLambdaHandler() func(context.Context) error {
	return func(ctx context.Context) error {
		c.Color = false // disable color output for Lambda
		c.command = os.Getenv("ECRM_COMMAND")
		return c.Run(ctx)
	}
}
