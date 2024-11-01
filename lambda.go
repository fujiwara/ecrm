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
	"github.com/samber/lo"
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
		kept := int64(0)
		// scan the versions
		// 1. aliased versions
		// 2. latest keepCount versions
		scanVersions := lo.Filter(versions, func(v lambdaTypes.FunctionConfiguration, _ int) bool {
			if _, ok := aliases[*v.Version]; ok {
				return ok
			}
			kept++
			return kept < keepCount
		})
		for _, v := range scanVersions {
			if err := s.scanLambdaFunctionArn(ctx, *v.FunctionArn, aliases[*v.Version]...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Scanner) scanLambdaFunctionArn(ctx context.Context, functionArn string, aliasNames ...string) error {
	log.Println("[debug] Getting Lambda function ", functionArn)
	f, err := s.lambda.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: &functionArn,
	})
	if err != nil {
		return fmt.Errorf("failed to get lambda function %s: %w", functionArn, err)
	}
	u := ImageURI(aws.ToString(f.Code.ImageUri))
	if u == "" {
		// skip if the image URI is empty
		return nil
	}
	log.Println("[debug] ImageUri", u)
	if s.Images.Add(u, functionArn) {
		if len(aliasNames) == 0 {
			log.Printf("[info] %s is in use by Lambda function %s", u.String(), functionArn)
		} else {
			log.Printf("[info] %s is in use by Lambda function %s aliases:%v", u.String(), functionArn, aliasNames)
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
