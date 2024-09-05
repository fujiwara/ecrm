package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/fujiwara/ecrm"
)

var version = "current"

func main() {
	ctx := context.TODO()
	app, err := ecrm.New(ctx, os.Getenv("AWS_REGION"))
	if err != nil {
		log.Fatal(err)
	}
	app.Version = version
	cli := app.NewCLI()
	if isLambda() && os.Getenv("ECRM_NO_LAMBDA_BOOTSTRAP") == "" {
		handler := cli.NewLambdaHandler()
		lambda.Start(handler)
		panic("unreachable here")
	}
	if err := cli.RunContext(ctx); err != nil {
		log.Fatal(err)
	}
}

func isLambda() bool {
	if strings.HasPrefix(os.Getenv("AWS_EXECUTION_ENV"), "AWS_Lambda") || os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		return true
	}
	return false
}
