// Command guardrail is the operator CLI: set up an MSP's Jennah datasets, and
// pull, validate, diff and publish config versions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alphauslabs/aws-spend-guardrail/config"
	"github.com/alphauslabs/aws-spend-guardrail/internal/awsx"
	"github.com/alphauslabs/aws-spend-guardrail/internal/cli"
	"github.com/alphauslabs/aws-spend-guardrail/internal/jennah"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
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
  guardrail preflight --msp ID FILE   (run with AWS credentials for the payer account)
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
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	msp := fs.String("msp", "", "MSP id")
	endpoint := fs.String("endpoint", os.Getenv("JENNAH_ENDPOINT"), "Jennah gRPC endpoint (default: the public endpoint)")
	apiKey := fs.String("api-key", os.Getenv("JENNAH_API_KEY"), "Jennah API key")
	ver := fs.Int64("version", 0, "config version (pull; default newest)")
	out := fs.String("o", "", "write the document to FILE (pull)")
	note := fs.String("note", "", "note stored with the version (publish)")
	payer := fs.String("payer", "", "payer (management) account of the org")
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
		return preflight(ctx, raw, *msp, stdout)
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
	if full > 0 {
		return fmt.Errorf("%d target(s) have no free SCP slot; detach or merge an SCP on each before installing", full)
	}
	return nil
}
