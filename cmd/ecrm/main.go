package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/fujiwara/ecrm"
)

func main() {
	c := &ecrm.Command{}
	flag.IntVar(&c.Keeps, "keeps", 1, "keep outdated revisions")
	flag.Parse()

	app, err := ecrm.New(context.Background(), os.Getenv("AWS_REGION"))
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Run(c); err != nil {
		log.Fatal(err)
	}
}
