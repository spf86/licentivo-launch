# Metered business tasks through Launch

Launch Application exposes the embedded official SDK's reserve → business callback → commit/cancel workflow in all nine languages. Ordinary `RunFeature` / `runFeature` / `run_feature` checks authorization; it does not charge metered quantity. Do not use it as a replacement for a metered task.

Use one durable operation ID for each real business job. Retrying that job, even after restarting the process, must reuse the same ID, feature and quantity. Store the job ID with your own business record. Keep the SDK's private task journal and cache; do not delete them to force a retry.

| Language | Run a metered job on a started Application | Resolve a known interrupted outcome |
|---|---|---|
| Go | `app.RunMeteredFeature(ctx, "export", 1, jobID, action)` | `app.ResolveMeteredFeature(ctx, jobID, succeeded)` |
| Python | `app.run_metered_feature("export", 1, job_id, action)` | `app.resolve_metered_feature(job_id, succeeded)` |
| JavaScript | `await app.runMeteredFeature("export", 1, jobID, action)` | `await app.resolveMeteredFeature(jobID, succeeded)` |
| C# | `await app.RunMeteredFeature("export", 1, jobID, action, ct)` | `await app.ResolveMeteredFeature(jobID, succeeded, ct)` |
| Java | `app.runMeteredFeature("export", 1, jobID, action)` | `app.resolveMeteredFeature(jobID, succeeded)` |
| Ruby | `app.run_metered_feature("export", 1, job_id) { business }` | `app.resolve_metered_feature(job_id, succeeded)` |
| Rust | `app.run_metered_feature("export", 1, job_id, action)?` | `app.resolve_metered_feature(job_id, succeeded)?` |
| C | `launch_application_run_metered_feature(app, "export", 1, job_id, action, context)` | `launch_application_resolve_metered_feature(app, job_id, succeeded)` |
| C++ | `app.RunMeteredFeature("export", 1, job_id, action)` | `app.ResolveMeteredFeature(job_id, succeeded)` |

The official SDK owns authorization, quota, journals, settlement and any supported reservation renewal. Launch forwards these calls to the same runtime that was started at login/restore. Custom runtime adapters must implement the optional metered methods. Unsupported adapters fail explicitly and never fall back to an ordinary feature callback.

A committed replay succeeds without executing business again. Rust returns `MeteredExecution::AlreadyCompleted`; an actual execution returns `MeteredExecution::Executed(value)`. Python and JavaScript may return no value on a completed replay. Store reusable business results in your own durable job record instead of depending on callback return values during recovery.

If the process stops during a callback, inspect the actual business output before resolving. Pass `true` only if work succeeded, `false` only if it did not. Resolving never executes business. An uncertain settlement must remain pending; do not assign a new ID or delete journals to rerun completed work. Reserve/commit errors do not prove the business callback failed. Use idempotent business writes and preserve their outcome independently.

These APIs describe the current source. Check the installed package's methods and artifact checksum; a matching version string does not prove a public release contains uncommitted fixes. See [compatibility and release evidence](sdk-compatibility.md).

## Long-task boundary

The embedded Python SDK includes automatic reservation renewal from the N09 fix. The other eight language paths forward their existing official SDK implementation; they do not gain a renewal worker from this Launch change. Their short-task execution and replay tests do not certify work longer than the default 15-minute reservation. Treat long-task renewal in those official SDKs as a separate upstream gap; Launch must not silently invent a second quota protocol.
