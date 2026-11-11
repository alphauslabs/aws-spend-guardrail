package config

import "testing"

var acct = Target{Kind: "account", ID: "111122223333"}

func base() ResolvedScope {
	return ResolvedScope{
		Target:       acct,
		CeilingUSD:   50000,
		Period:       PeriodMonthly,
		ThresholdPct: 90,
		FreezeStyle:  StyleA,
		Mode:         ModeDryRun,
		MonthEnd:     MonthEndAutoLift,
	}
}

func cfg(scopes ...ResolvedScope) *Resolved {
	r := &Resolved{PayerAccountID: "123456789012", Scopes: scopes}
	r.sort()
	return r
}

func TestFieldClassification(t *testing.T) {
	cases := []struct {
		name   string
		change func(*ResolvedScope)
		field  string
		dir    Direction
	}{
		{"lower ceiling", func(s *ResolvedScope) { s.CeilingUSD = 40000 }, "ceiling_usd", Tightening},
		{"higher ceiling", func(s *ResolvedScope) { s.CeilingUSD = 60000 }, "ceiling_usd", Loosening},
		{"lower threshold", func(s *ResolvedScope) { s.ThresholdPct = 80 }, "threshold_pct", Tightening},
		{"higher threshold", func(s *ResolvedScope) { s.ThresholdPct = 95 }, "threshold_pct", Loosening},
		{"to enforce", func(s *ResolvedScope) { s.Mode = ModeEnforce }, "mode", Tightening},
		{"to style B", func(s *ResolvedScope) { s.FreezeStyle = StyleB }, "freeze_style", Tightening},
		{"to stay_frozen", func(s *ResolvedScope) { s.MonthEnd = MonthEndStayFrozen }, "month_end", Tightening},
		{"different period", func(s *ResolvedScope) { s.Period = PeriodQuarterly }, "period", Loosening},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := base()
			c.change(&d)
			got := Diff(cfg(base()), cfg(d))
			if len(got) != 1 || got[0].Field != c.field || got[0].Direction != c.dir {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestReverseMovesAreLoosening(t *testing.T) {
	a := base()
	a.Mode, a.FreezeStyle, a.MonthEnd = ModeEnforce, StyleB, MonthEndStayFrozen
	d := base() // dry-run, A, auto_lift
	for _, c := range Diff(cfg(a), cfg(d)) {
		if c.Direction != Loosening {
			t.Fatalf("%v should be loosening", c)
		}
	}
}

func TestAddedAndRemovedScopes(t *testing.T) {
	other := base()
	other.Target = Target{Kind: "ou", ID: "ou-ab12-cd34ef56"}

	added := Diff(cfg(base()), cfg(base(), other))
	if len(added) != 1 || added[0].Field != "scope" || added[0].Direction != Tightening {
		t.Fatalf("added: %v", added)
	}
	removed := Diff(cfg(base(), other), cfg(base()))
	if len(removed) != 1 || removed[0].Field != "scope" || removed[0].Direction != Loosening {
		t.Fatalf("removed: %v", removed)
	}
}

func TestFirstConfigIsAllTightening(t *testing.T) {
	if HasLoosening(Diff(nil, cfg(base()))) {
		t.Fatal("a first config should apply at once")
	}
}

func TestStricterOfMixedVersion(t *testing.T) {
	x := base()
	y := base()
	y.Target = Target{Kind: "account", ID: "444455556666"}
	applied := cfg(x, y)

	newX, newY := x, y
	newX.CeilingUSD = 40000 // tightening
	newY.CeilingUSD = 60000 // loosening
	desired := cfg(newX, newY)

	changes := Diff(applied, desired)
	if !HasLoosening(changes) {
		t.Fatal("expected a loosening")
	}
	got := Stricter(applied, desired)
	gx, _ := got.Scope(x.Target)
	gy, _ := got.Scope(y.Target)
	if gx.CeilingUSD != 40000 {
		t.Fatalf("X should take the lower ceiling at once, got %v", gx.CeilingUSD)
	}
	if gy.CeilingUSD != 50000 {
		t.Fatalf("Y should keep its old ceiling, got %v", gy.CeilingUSD)
	}
}

func TestStricterKeepsRemovedScopeAndAddsNewOne(t *testing.T) {
	x := base()
	y := base()
	y.Target = Target{Kind: "account", ID: "444455556666"}
	got := Stricter(cfg(x), cfg(y))
	if _, ok := got.Scope(x.Target); !ok {
		t.Fatal("removed scope must be kept until approved")
	}
	if _, ok := got.Scope(y.Target); !ok {
		t.Fatal("added scope must apply at once")
	}
}

func TestStricterKeepsPeriodUntilApproved(t *testing.T) {
	d := base()
	d.Period = PeriodAnnually
	got := Stricter(cfg(base()), cfg(d))
	if got.Scopes[0].Period != PeriodMonthly {
		t.Fatalf("period changed early: %v", got.Scopes[0].Period)
	}
}

func TestStricterOfPureTighteningIsDesired(t *testing.T) {
	d := base()
	d.CeilingUSD, d.Mode = 1000, ModeEnforce
	if !Equal(Stricter(cfg(base()), cfg(d)), cfg(d)) {
		t.Fatal("pure tightening should apply as published")
	}
}

func TestExclusionChangesAreNotices(t *testing.T) {
	a := cfg(base())
	d := cfg(base())
	d.ExcludedAccounts = []string{"999988887777"}
	ch := Diff(a, d)
	if len(ch) != 1 || ch[0].Direction != Notice || ch[0].Field != "excluded" || HasLoosening(ch) {
		t.Fatalf("adding an exclusion: %v", ch)
	}
	back := Diff(d, a)
	if len(back) != 1 || back[0].Direction != Notice || HasLoosening(back) {
		t.Fatalf("removing an exclusion: %v", back)
	}
	if got := Stricter(a, d); len(got.ExcludedAccounts) != 1 {
		t.Fatal("exclusions apply at once")
	}
	if Equal(a, d) {
		t.Fatal("configs differing only in exclusions are not equal")
	}
}

func withRecipients(r *Resolved, emails ...string) *Resolved {
	r.AlertEmails = emails
	return r
}

func TestRecipientChangesNeedApproval(t *testing.T) {
	a := withRecipients(cfg(base()), "oncall@example.com")

	added := Diff(a, withRecipients(cfg(base()), "finops@example.com", "oncall@example.com"))
	if len(added) != 1 || added[0].Field != FieldAlertEmail || added[0].To != "finops@example.com" || added[0].Direction != Loosening {
		t.Fatalf("adding a recipient: %v", added)
	}
	if !OnlyRecipients(added) {
		t.Fatal("a recipient-only version should be recognized as such")
	}
	if got := added[0].String(); got != "alert recipient added: finops@example.com (loosening)" {
		t.Errorf("String() = %q", got)
	}

	removed := Diff(a, cfg(base()))
	if len(removed) != 1 || removed[0].From != "oncall@example.com" || removed[0].Direction != Loosening {
		t.Fatalf("removing a recipient: %v", removed)
	}
	if got := removed[0].String(); got != "alert recipient removed: oncall@example.com (loosening)" {
		t.Errorf("String() = %q", got)
	}
}

func TestSameRecipientsInAnotherOrderOrCaseAreNoChange(t *testing.T) {
	doc := func(list string) *Resolved {
		d, err := Parse([]byte(`{"payer_account_id":"123456789012","scopes":[],"alert_emails":`+list+`}`), Options{})
		if err != nil {
			t.Fatal(err)
		}
		return d.Resolve()
	}
	a := doc(`["a@example.com","b@example.com"]`)
	d := doc(`["B@example.com","a@example.com"]`)
	if ch := Diff(a, d); len(ch) != 0 || !Equal(a, d) {
		t.Fatalf("reordered or recased recipients: %v", ch)
	}
}

func TestStricterKeepsRecipientsUntilApproved(t *testing.T) {
	x := base()
	a := withRecipients(cfg(x), "oncall@example.com")
	nx := x
	nx.CeilingUSD = 1000 // tightening
	d := withRecipients(cfg(nx), "finops@example.com")

	ch := Diff(a, d)
	// The tightening applies at once, so only recipient changes await approval.
	if !HasLoosening(ch) || !OnlyRecipients(ch) {
		t.Fatalf("mixed version: %v", ch)
	}
	y := x
	y.CeilingUSD = 90000 // loosening
	if OnlyRecipients(Diff(a, withRecipients(cfg(y), "finops@example.com"))) {
		t.Fatal("a version that also raises a ceiling is not recipient-only")
	}
	got := Stricter(a, d)
	if len(got.AlertEmails) != 1 || got.AlertEmails[0] != "oncall@example.com" {
		t.Fatalf("recipients changed before approval: %v", got.AlertEmails)
	}
	if got.Scopes[0].CeilingUSD != 1000 {
		t.Fatal("the tightening should apply at once")
	}
	if Equal(a, withRecipients(cfg(x), "finops@example.com")) {
		t.Fatal("configs differing only in recipients are not equal")
	}
}
