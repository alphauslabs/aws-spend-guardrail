# Design

## Context

See proposal.md (Why). `ai.Pack.PriceIn` returns a config `ai.prices` entry before anything else; below it, `inRegion` merges the release and shared tables tighten-only and raises a model only the shared tables list to its provider's floor. The daily run (`Guard.Daily` in `internal/guard/freeze.go`) already runs other once-a-day checks, and reported records (`State.Reported`, `MarkReported`, `ClearReported`) already dedupe one-time alerts. The pack for the config in force is built in `internal/guard/fast.go` with `ai.NewShared`.

## Goals / Non-Goals

**Goals:**
- Say when a config price has fallen below what the release or shared prices charge for the model, once per change.

**Non-Goals:**
- Changing pricing: the entry keeps winning. Retiring or capping it automatically would loosen or tighten an approved config without approval.
- Reporting entries above the known price: those only tighten, and an org may set them deliberately.
- Comparing against the provider floor: the floor is a conservative stand-in, not a price, and the operating guide tells orgs to price below it.

## Decisions

### D1: The known price is the lowest regional price, without floor or config

`ai.Pack` gains `KnownPrice(key) (config.ModelPrice, bool)`: for each region where the release or shared tables list the model, its price merged tighten-only as `inRegion` does but without the floor, then the lowest input and the lowest output across those regions, taken separately. `ok` is false when no table lists the model. An entry is low when its input is below the known input or its output below the known output.

Taking the lowest region avoids reporting an entry set at the us-east-1 price just because other regions cost more. Alternative: compare per region. Rejected: most models cost about 10% more outside us-east-1, so nearly every entry would be reported.

### D2: Once a day, in the daily run

The check runs in `Daily`, only when the AI guard is on and a config is in force, after shared prices were pulled by the run's normal pull. It builds the pack for the config in force as `fast.go` does. A day is soon enough: the risk is a slow drift in AWS prices, not a fast breach.

### D3: Reported records keyed by model and prices

Each report is a reported record of kind `price_override_low` keyed `<model>|<entry>|<known>`, prices formatted as in alerts. A key already present is not reported again. Each run clears the records of this kind whose key isn't low today, so a raised, removed, or no-longer-low entry is forgotten without an alert, and a later return is reported again. The event id is `price_override_low:<key>`, so a retried write is the same event.

### D4: Alert text points at the fix

The alert names the payer org's model, the entry, and the known price, says calls are still priced at the entry, and says to raise or remove the entry in `ai.prices` (raising applies at once; removing needs approval). It passes `alerttext.Check` like every alert.

## Risks / Trade-offs

- [A known price that's itself wrong, such as a mistaken shared manual price, reports a correct entry as low] → The alert shows both prices; the fix is to correct the shared price, and the report clears on the next run.
- [Regional price differences hide a stale entry that's low only in pricier regions] → Accepted by D1; the guide already says an entry applies in every region.

## Migration Plan

Patch release, no template change. The first daily run after upgrading reports any entry already low. Rolling back leaves the reported records unused.
