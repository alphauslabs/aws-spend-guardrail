package cli

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/schema"
)

// consoleQuickCreate is the CloudFormation console's quick-create page in
// us-east-1, where the guard is always installed.
const consoleQuickCreate = "https://us-east-1.console.aws.amazon.com/cloudformation/home?region=us-east-1#/stacks/quickcreate"

// LinkOptions are the values an install link can fill in. MSP, Payer, Bucket
// and Prefix are required; the rest are left for the console form when empty.
type LinkOptions struct {
	MSP             string
	Payer           string
	Bucket          string // the client's bucket holding the release files
	Prefix          string // folder holding template.yaml and guard.zip, e.g. guard/v0.1.0
	Version         string
	ReaderSecretARN string
	WriterSecretARN string
	Approvers       []string
	AlertEmail      string
}

var (
	bucketRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	ipLikeRe  = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
	prefixRe  = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	versionRe = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
)

// InstallLink checks the options and returns a CloudFormation quick-create URL.
// It needs no credentials and makes no network calls.
func InstallLink(o LinkOptions) (string, error) {
	var probs []string
	add := func(format string, args ...any) { probs = append(probs, fmt.Sprintf(format, args...)) }

	// A key pasted into any option is the likely mistake; refuse it before
	// anything else so it never ends up in a URL.
	for _, v := range append([]string{o.MSP, o.Payer, o.Bucket, o.Prefix, o.Version, o.ReaderSecretARN, o.WriterSecretARN, o.AlertEmail}, o.Approvers...) {
		if strings.Contains(v, "jennah_sk_") {
			return "", errors.New("a Jennah API key was given as an option; keys belong in Secrets Manager, and the link takes only the secret's ARN")
		}
	}

	if err := schema.ValidateMSPID(o.MSP); err != nil {
		add("--msp: %v", err)
	}
	if err := schema.ValidatePayer(o.Payer); err != nil {
		add("--payer: %v", err)
	}
	if !bucketRe.MatchString(o.Bucket) || strings.Contains(o.Bucket, "..") || ipLikeRe.MatchString(o.Bucket) {
		add("--bucket %q is not a valid S3 bucket name", o.Bucket)
	}
	prefix := strings.Trim(o.Prefix, "/")
	if prefix == "" || !prefixRe.MatchString(prefix) || strings.Contains(prefix, "//") {
		add("--prefix %q must be a folder like guard/v0.1.0", o.Prefix)
	}
	if o.Version != "" && !versionRe.MatchString(o.Version) {
		add("--version %q has characters a release version can't have", o.Version)
	}
	for _, s := range []struct{ flag, arn string }{{"--reader-secret-arn", o.ReaderSecretARN}, {"--writer-secret-arn", o.WriterSecretARN}} {
		if s.arn == "" {
			continue
		}
		re := regexp.MustCompile(`^arn:aws:secretsmanager:us-east-1:` + regexp.QuoteMeta(o.Payer) + `:secret:[A-Za-z0-9/_+=.@-]+$`)
		if !re.MatchString(s.arn) {
			add("%s %q must be a Secrets Manager secret in us-east-1 of payer account %s", s.flag, s.arn, o.Payer)
		}
	}
	for _, a := range o.Approvers {
		if !isEmail(a) {
			add("--approvers: %q is not an email address", a)
		}
	}
	if o.AlertEmail != "" && !isEmail(o.AlertEmail) {
		add("--alert-email: %q is not an email address", o.AlertEmail)
	}
	if len(probs) > 0 {
		return "", errors.New("cannot make the link:\n  - " + strings.Join(probs, "\n  - "))
	}

	params := map[string]string{
		"MSPId":                  o.MSP,
		"ExpectedPayerAccountId": o.Payer,
		"FunctionPackageBucket":  o.Bucket,
		"FunctionPackageKey":     prefix + "/guard.zip",
	}
	set := func(k, v string) {
		if v != "" {
			params[k] = v
		}
	}
	set("GuardVersion", o.Version)
	set("ReaderSecretArn", o.ReaderSecretARN)
	set("WriterSecretArn", o.WriterSecretARN)
	set("LooseningApprovers", strings.Join(o.Approvers, ","))
	set("AlertEmail", o.AlertEmail)

	// Query parameters in a fixed order: templateURL, stackName, then the
	// parameters by name, so the same options always give the same link.
	var b strings.Builder
	b.WriteString(consoleQuickCreate)
	b.WriteString("?templateURL=")
	b.WriteString(url.QueryEscape(templateURL(o.Bucket, prefix)))
	b.WriteString("&stackName=")
	b.WriteString(url.QueryEscape(StackName(o.MSP)))
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		b.WriteString("&param_" + k + "=")
		b.WriteString(url.QueryEscape(params[k]))
	}
	return b.String(), nil
}

// StackName is the guard's stack name for an MSP. Stack names allow only
// letters, digits and hyphens, so underscores in the MSP id become hyphens.
func StackName(msp string) string { return "guardrail-" + strings.ReplaceAll(msp, "_", "-") }

// templateURL addresses the template in the client's bucket. A bucket name
// with dots can't use the virtual-hosted form over HTTPS, so it uses the path
// form.
func templateURL(bucket, prefix string) string {
	if strings.Contains(bucket, ".") {
		return "https://s3.us-east-1.amazonaws.com/" + bucket + "/" + prefix + "/template.yaml"
	}
	return "https://" + bucket + ".s3.us-east-1.amazonaws.com/" + prefix + "/template.yaml"
}

func isEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && strings.Contains(s, "@")
}
