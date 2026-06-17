---
name: audit-b11k-security
description: Run and interpret a full security audit workflow for the B11K Strava activity project, covering the Swift/iOS app, Go mobile backend/API, deployment config, secrets/residue, dependency vulnerabilities, SAST, mobile package analysis, and optional dynamic API/mobile testing. Use when Codex is asked to audit B11K security, check the iOS app or Go backend before publishing, investigate personal data or secret residue, prepare a security report, or run tools such as gosec, govulncheck, semgrep, mobsfscan, MobSF, gitleaks, Trivy, CodeQL, or ZAP for this repo.
---

# B11K Security Audit

## Start Here

Use this skill for B11K security reviews that touch the iOS app, Go backend, mobile API, deployment posture, or publishing readiness.

Before running commands:

1. Confirm the requested audit scope: source-only, dependency/SAST, mobile package, dynamic API, or full pass.
2. Treat local `.env`, tokens, Strava credentials, database URLs, deployment domains, device IDs, and IPs as sensitive.
3. Do not paste secret values into the final answer. Report the file, key name, and risk with values redacted.
4. Prefer reports under `/tmp` unless the user asks for repo artifacts.
5. Use read-only scans by default. Run active ZAP/API scans only against local/staging targets the user owns.

## Quick Runner

Run the bundled runner from the repo root:

```bash
bash /Users/username/.codex/skills/audit-b11k-security/scripts/run_b11k_security_audit.sh /Users/orange/git/b11k
```

The runner writes a timestamped report directory under `/tmp` and skips tools that are not installed. It does not run active ZAP or MobSF IPA analysis unless the relevant environment variables are set.

Useful optional environment variables:

```bash
B11K_AUDIT_OUT=/tmp/my-b11k-audit
B11K_AUDIT_XCODE_ANALYZE=1
B11K_IPA_PATH=/absolute/path/B11k.ipa
B11K_ZAP_TARGET=https://staging.example.com
B11K_SEMGREP_CONFIG=auto
B11K_AUDIT_TOOL_CHECK_ONLY=1
```

After the runner finishes, inspect `summary.txt` first, then review individual JSON/SARIF/log files. Treat scanner output as leads; validate findings against the code before reporting.

## Manual Review

Read `references/checklist.md` when doing a human review or writing the final audit report. It maps the B11K app/backend to mobile and API security areas:

- iOS storage, Keychain, UserDefaults migration, logs, ATS, URL schemes, API URL validation, background snapshots, privacy.
- Go mobile session auth, Strava token handling, encryption at rest, host/TLS policy, headers, rate limits, CORS/origin behavior, multi-user scoping.
- Publishing and deployment readiness.

Read `references/tools.md` when installing missing tools, choosing which scans to run, or explaining limitations.

## Expected Audit Order

1. Inventory and residue:
   Check tracked URLs, IPs, emails, personal names, bundle identifiers, Apple team IDs, ignored local files, and accidental config exposure.
2. Secrets:
   Run redacted secret scanning against git history and working files. Never reveal actual values.
3. Go backend:
   Run `go test`, `govulncheck`, `gosec`, Semgrep/CodeQL if available, and manual auth/session review.
4. iOS source:
   Run `mobsfscan`, Semgrep/CodeQL Swift if available, `xcodebuild analyze` when requested, and manual MASVS review.
5. Package/deployment:
   Analyze IPA with MobSF when provided. Check production config expectations, TLS, token encryption, and app signing metadata.
6. Dynamic API:
   Use ZAP baseline/API scans only for local or explicitly authorized staging/prod targets. For authenticated mobile API endpoints, use a disposable token where possible.
7. Triage:
   Group results by severity, exploitability, and publishing impact. Distinguish confirmed findings from scanner false positives and config warnings.

## Final Report Shape

Lead with findings by severity. Include:

- Finding title and severity.
- Affected file/endpoint/config with line references where possible.
- Why it matters for B11K.
- Concrete remediation.
- Tool evidence, if any.
- Residual risk or testing gap.

If no issue is found in an area, say so and name the evidence checked. Keep secret values redacted.
