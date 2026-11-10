package awsx

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	ltypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type fakeLambda struct {
	hash  string
	err   error
	asked string
}

func (f *fakeLambda) GetFunction(_ context.Context, in *lambda.GetFunctionInput, _ ...func(*lambda.Options)) (*lambda.GetFunctionOutput, error) {
	f.asked = aws.ToString(in.FunctionName)
	if f.err != nil {
		return nil, f.err
	}
	return &lambda.GetFunctionOutput{Configuration: &ltypes.FunctionConfiguration{CodeSha256: aws.String(f.hash)}}, nil
}

func TestCodeSHA256(t *testing.T) {
	f := &fakeLambda{hash: "q1w2e3="}
	got, err := CodeSHA256(context.Background(), f, "guardrail-msp_a")
	if err != nil || got != "q1w2e3=" || f.asked != "guardrail-msp_a" {
		t.Fatalf("got %q %v asked %q", got, err, f.asked)
	}
	if _, err := CodeSHA256(context.Background(), &fakeLambda{err: errors.New("AccessDenied")}, "x"); err == nil {
		t.Fatal("an access error must surface")
	}
	if _, err := CodeSHA256(context.Background(), &fakeLambda{}, ""); err == nil {
		t.Fatal("no function name must fail")
	}
}
