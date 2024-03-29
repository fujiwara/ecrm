package ecrm

import (
	"context"
	"log"
	"math"
	"sort"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdaTypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func (app *App) lambdaFunctions(ctx context.Context) ([]lambdaTypes.FunctionConfiguration, error) {
	fns := make([]lambdaTypes.FunctionConfiguration, 0)
	p := lambda.NewListFunctionsPaginator(app.lambda, &lambda.ListFunctionsInput{})
	for p.HasMorePages() {
		r, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, fn := range r.Functions {
			if fn.PackageType != "Image" {
				continue
			}
			log.Printf("[debug] lambda function %s PackageType %s", *fn.FunctionName, fn.PackageType)
			fns = append(fns, fn)
		}
	}
	return fns, nil
}

func (app *App) scanLambdaFunctions(ctx context.Context, lcs []*LambdaConfig, images map[string]set) error {
	funcs, err := app.lambdaFunctions(ctx)
	if err != nil {
		return err
	}

	for _, fn := range funcs {
		var name string
		var keepCount int64
		for _, tc := range lcs {
			fname := *fn.FunctionName
			if tc.Match(fname) {
				name = fname
				keepCount = tc.KeepCount
				break
			}
		}
		if name == "" {
			continue
		}
		log.Printf("[debug] Checking Lambda function %s latest %d versions", name, keepCount)
		p := lambda.NewListVersionsByFunctionPaginator(
			app.lambda,
			&lambda.ListVersionsByFunctionInput{
				FunctionName: fn.FunctionName,
				MaxItems:     aws.Int32(int32(keepCount)),
			},
		)
		var versions []lambdaTypes.FunctionConfiguration
		for p.HasMorePages() {
			r, err := p.NextPage(ctx)
			if err != nil {
				return err
			}
			versions = append(versions, r.Versions...)
		}
		sort.SliceStable(versions, func(i, j int) bool {
			return lambdaVersionInt64(*versions[j].Version) < lambdaVersionInt64(*versions[i].Version)
		})
		if len(versions) > int(keepCount) {
			versions = versions[:int(keepCount)]
		}
		for _, v := range versions {
			log.Println("[debug] Getting Lambda function ", *v.FunctionArn)
			f, err := app.lambda.GetFunction(ctx, &lambda.GetFunctionInput{
				FunctionName: v.FunctionArn,
			})
			if err != nil {
				return err
			}
			img := aws.ToString(f.Code.ImageUri)
			if img == "" {
				continue
			}
			log.Println("[debug] ImageUri", img)
			if images[img] == nil {
				images[img] = newSet()
			}
			log.Printf("[info] %s is in use by Lambda function %s:%s", img, *v.FunctionName, *v.Version)
			images[img].add(*v.FunctionArn)
		}
	}
	return nil
}

func lambdaVersionInt64(v string) int64 {
	var vi int64
	if v == "$LATEST" {
		vi = math.MaxInt64
	} else {
		vi, _ = strconv.ParseInt(v, 10, 64)
	}
	return vi
}
