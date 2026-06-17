# B11K Security Audit Tools

Use installed tools first. If a tool is missing, report it as skipped unless the user approved installing it.

## Source And Secret Scans

- `gitleaks`: preferred first pass for secrets in git history and working files. Use `--redact=100`.
- `trufflehog`: optional second pass. Use `--no-verification` unless the user explicitly wants online credential verification.
- `rg` or `git grep`: useful for residue searches, but do not print real secret values.

## Go Backend

- `go test ./...`: baseline correctness before interpreting scanner results.
- `govulncheck ./...`: official Go vulnerability reachability analysis.
- `gosec ./...`: Go SAST for common security issues, crypto/TLS mistakes, injection, unsafe file handling, and taint flows.
- `semgrep`: cross-language SAST. Use `SEMGREP_SEND_METRICS=off` for local scans.
- `osv-scanner` or `trivy fs`: dependency, filesystem, SBOM, and misconfiguration checks.
- `CodeQL`: deeper Go analysis when the CLI/database is available.

## iOS App

- `mobsfscan --type ios`: source-level mobile security rules for Swift/Objective-C.
- `semgrep`: Swift SAST when rules are available.
- `xcodebuild analyze`: Apple/Clang/Xcode static analyzer. Set `CODE_SIGNING_ALLOWED=NO` and a temporary `-derivedDataPath`.
- `CodeQL`: Swift analysis when available on macOS.
- `MobSF`: IPA/source mobile assessment for ATS, binary/package metadata, endpoints, permissions/entitlements, privacy, and optional dynamic testing.

## Dynamic API And Web

- `ZAP baseline`: passive web/API scan. Suitable for local/staging and conservative production checks when authorized.
- Authenticated mobile API tests should use short-lived or disposable credentials and redact tokens from reports.

## Runner Behavior

The bundled runner:

- Writes under `/tmp` by default.
- Runs only tools present in `PATH`.
- Captures output to files rather than chat.
- Skips active ZAP unless `B11K_ZAP_TARGET` is set.
- Skips MobSF IPA upload unless `B11K_IPA_PATH` is set.
- Skips Xcode analyze unless `B11K_AUDIT_XCODE_ANALYZE=1` is set.
