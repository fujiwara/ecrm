package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/fujiwara/ecrm"
)

var version = "current"

func main() {
	ctx := context.TODO()
	app, err := ecrm.New(ctx, os.Getenv("AWS_REGION"))
	if err != nil {
		log.Fatal(err)
	}
	cliApp := app.NewCLI()
	cliApp.Version = version
	if isLambda() && os.Getenv("ECRM_NO_LAMBDA_BOOTSTRAP") == "" {
		cliApp.Action = app.NewLambdaAction()
	}
	if err := cliApp.RunContext(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

func isLambda() bool {
	if strings.HasPrefix(os.Getenv("AWS_EXECUTION_ENV"), "AWS_Lambda") || os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		return true
	}
	return false
}
