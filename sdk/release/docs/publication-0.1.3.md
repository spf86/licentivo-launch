# Launch SDK 0.1.3

This release embeds Licentivo SDK 0.11.2 in each of the nine language packages.

It includes durable account logout and retry state, account-store coordination, improved application restore behavior and the matching upstream logout protections. The upstream SDK independently persists binding-scoped logout records, preserves newer shared caches and refuses a logged-out binding when an old login rotates its identity JWT.

The server counterpart is Launch 0.3.1-beta with the matching current Licentivo server. Public package installation and GitHub artifacts are verified as separate publication steps. The final per-registry results are recorded in registry-status.json and registry-verification.json.
