- closes #282
- closes #284
- closes #285
- closes #286
- closes #499
- closes #500
- closes #501
- closes #505

### Changes Made:

#### docs: reference page for token_supply_change (#499)
- **Created** `docs/rules/token-supply-change.md` - Complete documentation page covering:
  - What events `token_supply_change` matches (mint, burn, any) and what doesn't (clawback, transfer, set_admin)
  - Parameters: `direction` (mint/burn/any) and `min_amount` (decimal string threshold)
  - How it relates to `token_event` with event: mint
  - Amounts as decimal strings (i128 precision)
  - Examples with curl commands
  - Validation rules
- **Updated** `docs/rules/params.md` - Added `token_supply_change` params table
- **Updated** `docs/SUMMARY.md` - Added `token_supply_change` to rule reference table  
- **Updated** `docs/guides/monitoring-a-token.md` - Added explanation linking `token_supply_change` to `token_event`

#### test(secrets): cover the env provider, IsReference, WithTTL and Schemes (#500)
- **Added** `TestIsReference` - Table-driven test covering supported schemes, unknown schemes, values containing `://`, empty strings
- **Added** `TestNewEnvProvider` - Tests `NewEnvProvider` resolves set variables and reports unset ones as errors
- **Added** `TestWithTTL` - Tests `WithTTL` sets cache lifetime and non-positive durations disable caching
- **Added** `TestSchemes` - Tests `Schemes()` returns registered provider names
- **Added** `TestVaultProviderScheme` - Tests Vault provider's `Scheme()` returns "vault"

#### test(stellar): cover partsToBig at the int128 and int256 boundaries (#501)
- **Added** `TestPartsToBig` - Tests boundary values:
  - Zero, one, math.MaxInt64, math.MinInt64 as high parts
  - Single and two rest parts (1, 2 → 2^64 + 2)
  - Amount 1000000 threshold
  - I256 all parts zero
  - Negative one

#### test(notify): cover the dispatcher's DisableAfterFailures and WithMetrics options (#505)
- **Added** `TestDispatchDisableAfterFailuresBasic` - Tests:
  - Default (0) never auto-disables
  - Threshold 4 with 404 permanent failures
  - Threshold 1 with single failure
- **Added** `TestDispatchDisableAfterFailuresThreshold` - Tests `WithDisableAfterFailures(4)` sets threshold
- **Added** `TestDispatchWithMetricsNilSafe` - Tests `WithMetrics(nil)` is nil-safe
