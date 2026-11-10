package awsx

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// GetFunctionAPI is the part of the Lambda client used to read the guard's
// own code hash.
type GetFunctionAPI interface {
	GetFunction(context.Context, *lambda.GetFunctionInput, ...func(*lambda.Options)) (*lambda.GetFunctionOutput, error)
}

// CodeSHA256 returns a function's deployed package hash, in the base64 form
// Lambda reports and release notes publish.
func CodeSHA256(ctx context.Context, api GetFunctionAPI, function string) (string, error) {
	if function == "" {
		return "", errors.New("function name unknown")
	}
	out, err := api.GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(function)})
	if err != nil {
		return "", err
	}
	if out.Configuration == nil || aws.ToString(out.Configuration.CodeSha256) == "" {
		return "", errors.New("GetFunction returned no code hash")
	}
	return aws.ToString(out.Configuration.CodeSha256), nil
}
