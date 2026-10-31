# Real joint regression programs

These are the exact small client programs run against the isolated 177 lab after the fixes. They read existing private configuration and credentials from `/home/spf86/n-fixes-20261101`; no credential values are included here. This is an acceptance harness, not an installer or a production application.

`language_matrix.py` runs the nine language clients against fixed and floating entitlements. Each uses the public Launch Application for login, protected file output, denial, restore and logout. The fixed run also executes one metered job twice and resolves its receipt, asserting that the business callback runs once. `n06-ledger.json` independently confirms the nine committed one-unit server records.

`fork_regression.py` tests the official Python runtime via the private embedded module to reproduce N04. `long_regression.py` runs a real default-TTL task through Launch Application for over 15 minutes and writes the included CSV. `settlement_regression.py` terminates a real business process before returning from its callback, then verifies known-outcome recovery, cancellation and quota denial through Application.

The lab must already contain its public JSON, private state and credentials, the encrypted-store helper `exporter.py`, built client binaries and current SDK source. Run these scripts only in that dedicated test lab. They create test business files and authorization/usage records. The full reproduction environment remains in the private `.local/n-fixes-20261101` workspace and `/home/spf86/n-fixes-20261101` on 177.
