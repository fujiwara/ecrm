package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/fujiwara/ecrm"
	"github.com/k1LoW/duration"
)

var DefaultExpires = time.Hour * 24 * 30 * 3 // 3 months

func main() {
	var repo string
	var expires string
	var delete bool
	var force bool
	ex := DefaultExpires

	flag.StringVar(&repo, "repository", "", "ECR repository name")
	flag.StringVar(&expires, "expires", "", "expiration time")
	flag.BoolVar(&delete, "delete", false, "really　delete images")
	flag.BoolVar(&force, "force", false, "delete withtout confirmation")
	flag.Parse()

	if expires != "" {
		var err error
		ex, err = duration.Parse(expires)
		if err != nil {
			log.Fatal(err)
		}
	}
	c := ecrm.Command{
		Repository: repo,
		Expires:    ex,
		Delete:     delete,
	}

	app, err := ecrm.New(context.Background(), os.Getenv("AWS_REGION"))
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Run(&c); err != nil {
		log.Fatal(err)
	}
}
