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

func (app *App) scanLambdaFunctions(ctx context.Context, lcs []*LambdaConfig) (Images, error) {
	funcs, err := app.lambdaFunctions(ctx)
	if err != nil {
		return nil, err
	}
	images := make(Images)

	for _, fn := range funcs {
		var name string
		var keepCount int64
		for _, tc := range lcs {
			fn := *fn.FunctionName
			if tc.Match(fn) {
				name = fn
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
				return nil, err
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
				return nil, err
			}
			u := ImageURI(aws.ToString(f.Code.ImageUri))
			if u == "" {
				continue
			}
			log.Println("[debug] ImageUri", u)
			if images.Add(u, aws.ToString(v.FunctionArn)) {
				log.Printf("[info] %s is in use by Lambda function %s:%s", u.String(), *v.FunctionName, *v.Version)
			}
		}
	}
	return images, nil
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
