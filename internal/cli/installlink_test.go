package cli

import (
	"net/url"
	"strings"
	"testing"
)

func full() LinkOptions {
	return LinkOptions{
		MSP:             "msp_a",
		Payer:           "123456789012",
		Bucket:          "guardrail-artifacts-123456789012",
		Prefix:          "guard/v0.1.0",
		Version:         "v0.1.0",
		ReaderSecretARN: "arn:aws:secretsmanager:us-east-1:123456789012:secret:guardrail/msp_a/reader-AbCd12",
		WriterSecretARN: "arn:aws:secretsmanager:us-east-1:123456789012:secret:guardrail/msp_a/writer-EfGh34",
		Approvers:       []string{"a@example.com", "b@example.com"},
		AlertEmail:      "oncall@example.com",
	}
}

// params parses a quick-create link back into its query values.
func params(t *testing.T, link string) url.Values {
	t.Helper()
	_, query, ok := strings.Cut(link, "#/stacks/quickcreate?")
	if !ok || !strings.HasPrefix(link, consoleQuickCreate) {
		t.Fatalf("not a quick-create link: %s", link)
	}
	v, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestFullLink(t *testing.T) {
	link, err := InstallLink(full())
	if err != nil {
		t.Fatal(err)
	}
	p := params(t, link)
	want := map[string]string{
		"templateURL":                  "https://guardrail-artifacts-123456789012.s3.us-east-1.amazonaws.com/guard/v0.1.0/template.yaml",
		"stackName":                    "guardrail-msp-a",
		"param_MSPId":                  "msp_a",
		"param_ExpectedPayerAccountId": "123456789012",
		"param_FunctionPackageBucket":  "guardrail-artifacts-123456789012",
		"param_FunctionPackageKey":     "guard/v0.1.0/guard.zip",
		"param_GuardVersion":           "v0.1.0",
		"param_ReaderSecretArn":        full().ReaderSecretARN,
		"param_WriterSecretArn":        full().WriterSecretARN,
		"param_LooseningApprovers":     "a@example.com,b@example.com",
		"param_AlertEmail":             "oncall@example.com",
	}
	for k, v := range want {
		if p.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, p.Get(k), v)
		}
	}
	if len(p) != len(want) {
		t.Errorf("unexpected extra parameters: %v", p)
	}
	if len(link) >= 2000 {
		t.Errorf("link is %d characters, want under 2000", len(link))
	}
	again, _ := InstallLink(full())
	if again != link {
		t.Error("the same options should give the same link")
	}
}

func TestMinimalLinkLeavesTheRestForTheForm(t *testing.T) {
	link, err := InstallLink(LinkOptions{MSP: "msp_a", Payer: "123456789012", Bucket: "bkt-1", Prefix: "/guard/v0.1.0/"})
	if err != nil {
		t.Fatal(err)
	}
	p := params(t, link)
	if p.Get("param_FunctionPackageKey") != "guard/v0.1.0/guard.zip" {
		t.Errorf("prefix slashes not trimmed: %q", p.Get("param_FunctionPackageKey"))
	}
	for _, k := range []string{"param_ReaderSecretArn", "param_WriterSecretArn", "param_LooseningApprovers", "param_AlertEmail", "param_GuardVersion"} {
		if p.Has(k) {
			t.Errorf("%s should be left for the form", k)
		}
	}
}

func TestDottedBucketUsesPathStyle(t *testing.T) {
	o := full()
	o.Bucket = "guardrail.artifacts.example"
	link, err := InstallLink(o)
	if err != nil {
		t.Fatal(err)
	}
	if got := params(t, link).Get("templateURL"); got != "https://s3.us-east-1.amazonaws.com/guardrail.artifacts.example/guard/v0.1.0/template.yaml" {
		t.Fatalf("templateURL %q", got)
	}
}

func TestRefusals(t *testing.T) {
	cases := []struct {
		name   string
		change func(*LinkOptions)
		want   string
	}{
		{"key in a secret option", func(o *LinkOptions) { o.ReaderSecretARN = "jennah_sk_abcdef0123456789" }, "keys belong in Secrets Manager"},
		{"key in another option", func(o *LinkOptions) { o.AlertEmail = "jennah_sk_x@example.com" }, "keys belong in Secrets Manager"},
		{"bad msp", func(o *LinkOptions) { o.MSP = "MSP.A" }, "--msp"},
		{"bad payer", func(o *LinkOptions) { o.Payer = "12345" }, "--payer"},
		{"bad bucket", func(o *LinkOptions) { o.Bucket = "Bad_Bucket" }, "--bucket"},
		{"ip bucket", func(o *LinkOptions) { o.Bucket = "192.168.1.1" }, "--bucket"},
		{"no prefix", func(o *LinkOptions) { o.Prefix = "/" }, "--prefix"},
		{"secret in another account", func(o *LinkOptions) {
			o.ReaderSecretARN = "arn:aws:secretsmanager:us-east-1:999999999999:secret:guardrail/msp_a/reader-AbCd12"
		}, "--reader-secret-arn"},
		{"secret in another region", func(o *LinkOptions) {
			o.WriterSecretARN = "arn:aws:secretsmanager:eu-west-1:123456789012:secret:guardrail/msp_a/writer-EfGh34"
		}, "--writer-secret-arn"},
		{"not a secret arn", func(o *LinkOptions) { o.ReaderSecretARN = "arn:aws:s3:::bucket" }, "--reader-secret-arn"},
		{"bad approver", func(o *LinkOptions) { o.Approvers = []string{"a@example.com", "not-an-email"} }, "--approvers"},
		{"bad alert email", func(o *LinkOptions) { o.AlertEmail = "Ops <ops@example.com>" }, "--alert-email"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := full()
			c.change(&o)
			link, err := InstallLink(o)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got link=%q err=%v", c.want, link, err)
			}
			if link != "" {
				t.Fatal("no link may be printed on a refusal")
			}
		})
	}
}
