package ecrm

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdaTypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func (s *Scanner) scanLambdaFunctions(ctx context.Context, lcs []*LambdaConfig) error {
	funcs, err := lambdaFunctions(ctx, s.lambda)
	if err != nil {
		return err
	}

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
		aliases, err := s.getLambdaAliases(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to get lambda aliases: %w", err)
		}
		p := lambda.NewListVersionsByFunctionPaginator(
			s.lambda,
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
		var kept int64
		for _, v := range versions {
			log.Println("[debug] Getting Lambda function ", *v.FunctionArn)
			f, err := s.lambda.GetFunction(ctx, &lambda.GetFunctionInput{
				FunctionName: v.FunctionArn,
			})
			if err != nil {
				return err
			}
			u := ImageURI(aws.ToString(f.Code.ImageUri))
			if u == "" {
				continue
			}
			log.Println("[debug] ImageUri", u)
			if a, ok := aliases[*v.Version]; ok { // check if the version is aliased
				if s.Images.Add(u, aws.ToString(v.FunctionArn)) {
					log.Printf("[info] %s is in use by Lambda function %s:%s (alias=%v)", u.String(), *v.FunctionName, *v.Version, a)
				}
				continue
			}
			if kept >= keepCount {
				continue
			}
			if s.Images.Add(u, aws.ToString(v.FunctionArn)) {
				log.Printf("[info] %s is in use by Lambda function %s:%s", u.String(), *v.FunctionName, *v.Version)
				kept++
			}
		}
	}
	return nil
}

func (s *Scanner) getLambdaAliases(ctx context.Context, name string) (map[string][]string, error) {
	aliases := make(map[string][]string)
	var nextAliasMarker *string
	for {
		res, err := s.lambda.ListAliases(ctx, &lambda.ListAliasesInput{
			FunctionName: &name,
			Marker:       nextAliasMarker,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list aliases: %w", err)
		}
		for _, alias := range res.Aliases {
			aliases[*alias.FunctionVersion] = append(aliases[*alias.FunctionVersion], *alias.Name)
			if alias.RoutingConfig == nil || alias.RoutingConfig.AdditionalVersionWeights == nil {
				continue
			}
			for v := range alias.RoutingConfig.AdditionalVersionWeights {
				aliases[v] = append(aliases[v], *alias.Name)
			}
		}
		if nextAliasMarker = res.NextMarker; nextAliasMarker == nil {
			break
		}
	}
	return aliases, nil
}

func lambdaVersionInt64(v string) int64 {
	var vi int64
	if v == "$LATEST" {
		vi = math.MaxInt64
	} else {
		var err error
		vi, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			panic(fmt.Sprintf("invalid version number:%s %s", v, err.Error()))
		}
	}
	return vi
}
