// Command guardrail is the operator CLI: set up an MSP's Jennah datasets, and
// pull, validate, diff and publish config versions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/activity"
	"github.com/alphauslabs/aws-spend-guardrail/internal/awsx"
	"github.com/alphauslabs/aws-spend-guardrail/internal/cli"
	"github.com/alphauslabs/aws-spend-guardrail/internal/guard"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	"github.com/alphauslabs/aws-spend-guardrail/internal/prices"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `guardrail: operate aws-spend-guardrail config in Jennah.

Usage:
  guardrail setup    --msp ID --payer ACCOUNT
  guardrail pull     --msp ID --payer ACCOUNT [--version N] [-o FILE]
  guardrail validate [--payer ACCOUNT] FILE
  guardrail diff     --msp ID --payer ACCOUNT FILE
  guardrail publish  --msp ID --payer ACCOUNT [--note TEXT] FILE
  guardrail preflight --msp ID [--ai --trail-bucket BUCKET [--trail-prefix P] [--trail-kms-key ARN]] FILE
                     (run with AWS credentials for the payer account)
  guardrail install-link --msp ID --payer ACCOUNT --bucket BUCKET --prefix guard/VERSION
                     [--version V] [--reader-secret-arn ARN] [--writer-secret-arn ARN]
                     [--approvers EMAIL,...] [--alert-email EMAIL]
                     (prints a CloudFormation console link; needs no credentials)
  guardrail prices build -o FILE   (build the AI price table from the AWS Price List API)
  guardrail prices publish [--dry-run] [--note TEXT]
                     (publish it to the enterprise's shared guardrail.prices; run once per enterprise)
  guardrail schema   (print the config JSON schema)
  guardrail version

An MSP can have several payer orgs. Each has its own guard and its own config,
so every Jennah command names the payer account with --payer.

Jennah connection, for every command except validate, preflight, schema and version:
  --endpoint (or JENNAH_ENDPOINT)   optional; defaults to Jennah's public endpoint
  --api-key  (or JENNAH_API_KEY)    optional; otherwise your jennah CLI login is used
`

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	if cmd == "install-link" {
		return installLink(rest, stdout, stderr)
	}
	if cmd == "prices" {
		return pricesCmd(ctx, rest, stdout, stderr)
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	msp := fs.String("msp", "", "MSP id")
	endpoint := fs.String("endpoint", os.Getenv("JENNAH_ENDPOINT"), "Jennah gRPC endpoint (default: the public endpoint)")
	apiKey := fs.String("api-key", os.Getenv("JENNAH_API_KEY"), "Jennah API key")
	ver := fs.Int64("version", 0, "config version (pull; default newest)")
	out := fs.String("o", "", "write the document to FILE (pull)")
	note := fs.String("note", "", "note stored with the version (publish)")
	payer := fs.String("payer", "", "payer (management) account of the org")
	aiCheck := fs.Bool("ai", false, "also check the organization trail for the AI guard (preflight)")
	trailBucket := fs.String("trail-bucket", "", "bucket the organization trail writes to (preflight --ai)")
	trailPrefix := fs.String("trail-prefix", "", "the trail's S3 key prefix, if any (preflight --ai)")
	trailKMS := fs.String("trail-kms-key", "", "KMS key the trail encrypts with, if any (preflight --ai)")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	switch cmd {
	case "version":
		fmt.Fprintln(stdout, version)
		return nil
	case "schema":
		_, err := stdout.Write(config.SchemaJSON)
		return err
	case "validate":
		raw, err := readFile(fs)
		if err != nil {
			return err
		}
		return (&cli.CLI{Out: stdout, Status: stderr}).Validate(raw, *payer)
	case "preflight":
		raw, err := readFile(fs)
		if err != nil {
			return err
		}
		if *msp == "" {
			return errors.New("--msp is required")
		}
		if err := preflight(ctx, raw, *msp, stdout); err != nil {
			return err
		}
		if !*aiCheck {
			return nil
		}
		if *trailBucket == "" {
			return errors.New("--trail-bucket is required with --ai")
		}
		return preflightAI(ctx, awsx.TrailCheck{Bucket: *trailBucket, Prefix: *trailPrefix, KMSKeyARN: *trailKMS}, stdout)
	case "setup", "pull", "diff", "publish":
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}

	if *msp == "" || *payer == "" {
		return errors.New("--msp and --payer are required")
	}
	j, err := jennah.Dial(*endpoint, *apiKey)
	if err != nil {
		return fmt.Errorf("connect to Jennah: %w", err)
	}
	c := &cli.CLI{J: j, MSP: *msp, Payer: *payer, Out: stdout, Status: stderr}

	switch cmd {
	case "setup":
		return c.Setup(ctx)
	case "pull":
		if *out != "" {
			f, err := os.Create(*out)
			if err != nil {
				return err
			}
			defer f.Close()
			c.Out = f
		}
		return c.Pull(ctx, *ver)
	case "diff":
		raw, err := readFile(fs)
		if err != nil {
			return err
		}
		return c.Diff(ctx, raw)
	default: // publish
		raw, err := readFile(fs)
		if err != nil {
			return err
		}
		return c.Publish(ctx, raw, *note)
	}
}

func readFile(fs *flag.FlagSet) ([]byte, error) {
	if fs.NArg() != 1 {
		return nil, fmt.Errorf("expected one FILE argument, got %q", strings.Join(fs.Args(), " "))
	}
	return os.ReadFile(fs.Arg(0))
}

// preflight reports scopes whose targets have no free SCP slot for a freeze
// policy. It reads the organization with the caller's AWS credentials.
func preflight(ctx context.Context, raw []byte, msp string, stdout io.Writer) error {
	doc, err := config.Parse(raw, config.Options{})
	if err != nil {
		return err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		return err
	}
	var targets []config.Target
	for _, s := range doc.Resolve().Scopes {
		targets = append(targets, s.Target)
	}
	names := map[string]bool{"guardrail-freeze-a-" + msp: true, "guardrail-freeze-b-" + msp: true}
	reports, err := awsx.CheckSCPSlots(ctx, organizations.NewFromConfig(cfg), targets, names)
	if err != nil {
		return err
	}
	full := 0
	for _, r := range reports {
		state := "ok"
		if r.Full {
			state = "NO FREE SLOT: a freeze policy cannot be attached"
			full++
		}
		fmt.Fprintf(stdout, "%-30s %d of %d SCPs  %s\n", r.Target, r.Attached, awsx.MaxSCPsPerTarget, state)
	}
	outside, err := awsx.BudgetActionsOutside(ctx, budgets.NewFromConfig(cfg), doc.PayerAccountID, guard.BudgetPrefix)
	if err != nil {
		return err
	}
	actionsErr := checkBudgetActions(outside, doc.Resolve().EnforcingScopes(), stdout)
	if full > 0 {
		return errors.Join(fmt.Errorf("%d target(s) have no free SCP slot; detach or merge an SCP on each before installing", full), actionsErr)
	}
	return actionsErr
}

// checkBudgetActions reports the payer account's room for budget actions: each
// enforcing scope needs one, and AWS allows config.MaxEnforcingScopes per account.
func checkBudgetActions(outside, enforcing int, stdout io.Writer) error {
	limit := config.MaxEnforcingScopes
	scopes := "scopes"
	if enforcing == 1 {
		scopes = "scope"
	}
	fmt.Fprintf(stdout, "budget actions: %d outside the guard, %d enforcing %s in this config, limit %d\n", outside, enforcing, scopes, limit)
	if outside+enforcing > limit {
		return fmt.Errorf("the payer account needs %d budget actions (%d outside the guard and %d for enforcing scopes), and AWS allows %d; "+
			"remove budget actions outside the guard, or enforce fewer scopes (group accounts into OU scopes, or set some to dry-run)",
			outside+enforcing, outside, enforcing, limit)
	}
	return nil
}

// installLink prints a CloudFormation quick-create link. It parses its own
// flags because --version here is a release version, not a config version.
func installLink(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("install-link", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o cli.LinkOptions
	var approvers string
	fs.StringVar(&o.MSP, "msp", "", "MSP id")
	fs.StringVar(&o.Payer, "payer", "", "payer (management) account the stack is for")
	fs.StringVar(&o.Bucket, "bucket", "", "your S3 bucket in us-east-1 holding the release files")
	fs.StringVar(&o.Prefix, "prefix", "", "folder in the bucket with template.yaml and guard.zip, e.g. guard/v0.1.0")
	fs.StringVar(&o.Version, "version", "", "release version, recorded with every event")
	fs.StringVar(&o.ReaderSecretARN, "reader-secret-arn", "", "Secrets Manager ARN of the reader key")
	fs.StringVar(&o.WriterSecretARN, "writer-secret-arn", "", "Secrets Manager ARN of the writer key")
	fs.StringVar(&approvers, "approvers", "", "comma-separated emails that approve loosening")
	fs.StringVar(&o.AlertEmail, "alert-email", "", "email for local alerts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	for _, a := range strings.Split(approvers, ",") {
		if a = strings.TrimSpace(a); a != "" {
			o.Approvers = append(o.Approvers, a)
		}
	}
	link, err := cli.InstallLink(o)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, link)
	return nil
}

// preflightAI checks that the organization trail can feed the AI guard, then
// reads and decrypts one recent file of the management account. It runs with
// the operator's credentials: the guard's own access is confirmed by its first
// trail tick, which alerts on an access error.
func preflightAI(ctx context.Context, c awsx.TrailCheck, stdout io.Writer) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		return err
	}
	name, probs, err := awsx.CheckTrail(ctx, cloudtrail.NewFromConfig(cfg), c)
	if err != nil {
		return err
	}
	if len(probs) > 0 {
		return fmt.Errorf("the organization trail can't feed the AI guard:\n  - %s", strings.Join(probs, "\n  - "))
	}
	fmt.Fprintf(stdout, "organization trail %s: multi-region, logging, all management events\n", name)

	org, err := organizations.NewFromConfig(cfg).DescribeOrganization(ctx, &organizations.DescribeOrganizationInput{})
	if err != nil {
		return fmt.Errorf("read the organization: %w", err)
	}
	s3c := s3.NewFromConfig(cfg)
	loc, err := s3c.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: aws.String(c.Bucket)})
	if err != nil {
		return fmt.Errorf("find the bucket's region: %w", err)
	}
	if r := string(loc.LocationConstraint); r != "" {
		s3c = s3.NewFromConfig(cfg, func(o *s3.Options) { o.Region = r })
	}
	base := "AWSLogs/" + aws.ToString(org.Organization.Id) + "/"
	if c.Prefix != "" {
		base = strings.Trim(c.Prefix, "/") + "/" + base
	}
	bucket := &awsx.TrailBucket{API: s3c, Name: c.Bucket}
	mgmt := aws.ToString(org.Organization.MasterAccountId)
	now := time.Now().UTC()
	for _, day := range []time.Time{now, now.Add(-24 * time.Hour)} {
		objs, err := bucket.List(ctx, awsx.RecentTrailDay(base, mgmt, "us-east-1", day), "")
		if err != nil {
			return fmt.Errorf("list trail files: %w", err)
		}
		if len(objs) == 0 {
			continue
		}
		latest := objs[len(objs)-1]
		body, err := bucket.Get(ctx, latest.Key)
		if err != nil {
			return fmt.Errorf("read %s: %w", latest.Key, err)
		}
		recs, err := activity.ParseFile(body)
		if err != nil {
			return fmt.Errorf("parse %s: %w", latest.Key, err)
		}
		fmt.Fprintf(stdout, "read %s (%d records); the guard's own access is confirmed by its first run\n", latest.Key, len(recs))
		return nil
	}
	return fmt.Errorf("no trail file for the management account in us-east-1 today or yesterday under %s", base)
}

// pricesCmd builds the AI price table from the AWS Price List API (with the
// caller's AWS credentials), and writes it to a file or publishes it to the
// enterprise's shared prices dataset in Jennah.
func pricesCmd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || (args[0] != "build" && args[0] != "publish") {
		return fmt.Errorf("usage: guardrail prices build -o FILE | guardrail prices publish [--dry-run] [--note TEXT]")
	}
	sub := args[0]
	fs := flag.NewFlagSet("prices "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write the table to FILE (build)")
	dry := fs.Bool("dry-run", false, "show the changes without publishing (publish)")
	note := fs.String("note", "", "note stored with the price version (publish)")
	endpoint := fs.String("endpoint", os.Getenv("JENNAH_ENDPOINT"), "Jennah gRPC endpoint (default: the public endpoint)")
	apiKey := fs.String("api-key", os.Getenv("JENNAH_API_KEY"), "Jennah API key")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		return err
	}
	table, unmatched, err := prices.Fetch(ctx, pricing.NewFromConfig(cfg), bedrock.NewFromConfig(cfg))
	if err != nil {
		return err
	}
	if sub == "build" {
		if *out == "" {
			return errors.New("-o FILE is required")
		}
		raw, err := json.MarshalIndent(table, "", "  ")
		if err != nil {
			return err
		}
		for _, u := range unmatched {
			fmt.Fprintf(stderr, "no price found for %s (%s)\n", u.ID, u.Name)
		}
		fmt.Fprintf(stdout, "%d models priced, %d without a price, Price List published %s\n", len(table.Models), len(unmatched), table.Published)
		return os.WriteFile(*out, append(raw, '\n'), 0o644)
	}
	j, err := jennah.Dial(*endpoint, *apiKey)
	if err != nil {
		return fmt.Errorf("connect to Jennah: %w", err)
	}
	who, err := j.SDK().Auth.WhoAmI(ctx)
	if err != nil {
		return fmt.Errorf("find the active enterprise: %w", err)
	}
	c := &cli.CLI{J: j, Out: stdout, Status: stderr}
	return c.PublishPrices(ctx, who.GetIdentity().GetActiveEnterpriseId(), table, unmatched, *note, *dry)
}
