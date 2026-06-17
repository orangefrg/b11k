#!/usr/bin/env bash
set -u

REPO="${1:-$(pwd)}"
REPO="$(cd "$REPO" 2>/dev/null && pwd)"
if [ -z "$REPO" ] || [ ! -d "$REPO" ]; then
  echo "Usage: $0 /path/to/b11k" >&2
  exit 2
fi

if command -v go >/dev/null 2>&1; then
  GO_PATH="$(go env GOPATH 2>/dev/null || true)"
  if [ -n "$GO_PATH" ]; then
    PATH="$GO_PATH/bin:$PATH"
  fi
fi
PATH="$HOME/.local/bin:$HOME/Library/Python/3.9/bin:$HOME/Library/Python/3.10/bin:$HOME/Library/Python/3.11/bin:$PATH"
export PATH

STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="${B11K_AUDIT_OUT:-/tmp/b11k-security-audit-$STAMP}"
mkdir -p "$OUT"

export GOCACHE="${GOCACHE:-/tmp/b11k-go-cache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/b11k-go-mod-cache}"
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$OUT/xdg-config}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$OUT/xdg-cache}"
export SEMGREP_LOG_FILE="${SEMGREP_LOG_FILE:-$OUT/semgrep-user.log}"
export SEMGREP_SETTINGS_FILE="${SEMGREP_SETTINGS_FILE:-$OUT/semgrep-settings.yml}"
if [ -z "${SSL_CERT_FILE:-}" ] && [ -f /opt/homebrew/etc/ca-certificates/cert.pem ]; then
  export SSL_CERT_FILE=/opt/homebrew/etc/ca-certificates/cert.pem
fi

SUMMARY="$OUT/summary.txt"
: > "$SUMMARY"

note() {
  printf '%s\n' "$*" | tee -a "$SUMMARY"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

run_capture() {
  name="$1"
  outfile="$2"
  shift 2
  note "RUN $name"
  (
    cd "$REPO" || exit 2
    "$@"
  ) >"$outfile" 2>&1
  status=$?
  note "  exit=$status output=$outfile"
  return 0
}

run_json_report() {
  name="$1"
  tool="$2"
  outfile="$3"
  shift 3
  if have "$tool"; then
    run_capture "$name" "$outfile" "$@"
  else
    note "SKIP $name: '$tool' not found"
  fi
}

note "B11K security audit"
note "repo=$REPO"
note "out=$OUT"
note ""

run_capture "environment versions" "$OUT/versions.txt" bash -lc '
  set +e
  printf "date: "; date
  printf "git: "; git --version
  printf "go: "; go version
  printf "swift: "; swift --version | head -n 1
  printf "xcodebuild: "; xcodebuild -version | tr "\n" " "; printf "\n"
  for t in gitleaks trufflehog govulncheck gosec semgrep osv-scanner trivy mobsfscan codeql docker; do
    if command -v "$t" >/dev/null 2>&1; then
      printf "%s: %s\n" "$t" "$(command -v "$t")"
    else
      printf "%s: missing\n" "$t"
    fi
  done
'

if [ "${B11K_AUDIT_TOOL_CHECK_ONLY:-0}" = "1" ]; then
  note "Tool check only requested; skipping scans."
  note "Done. Tool inventory is in $OUT/versions.txt."
  exit 0
fi

run_capture "git status" "$OUT/git-status.txt" git status --short
run_capture "ignored files inventory" "$OUT/ignored-files.txt" git ls-files -o -i --exclude-standard
run_capture "tracked residue candidates" "$OUT/residue-tracked.txt" git grep -n -I -E 'https?://|[0-9]{1,3}(\.[0-9]{1,3}){3}|[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}' -- ':!LICENSE' ':!*.sum'
run_capture "tracked secret-keyword candidates" "$OUT/secret-keywords-tracked.txt" git grep -n -I -E 'client_secret|token_encryption_key|postgres_password|pg_secret|pg_password|api_key|private_key|begin .*private key|bearer [A-Za-z0-9_-]{20,}' -- ':!LICENSE'

if have gitleaks; then
  note "RUN gitleaks git redacted"
  (cd "$REPO" && gitleaks git --redact=100 --report-format json --report-path "$OUT/gitleaks-git.json" .) >"$OUT/gitleaks-git.log" 2>&1
  note "  exit=$? output=$OUT/gitleaks-git.json log=$OUT/gitleaks-git.log"
  note "RUN gitleaks dir redacted"
  (cd "$REPO" && gitleaks dir --redact=100 --report-format json --report-path "$OUT/gitleaks-dir.json" .) >"$OUT/gitleaks-dir.log" 2>&1
  note "  exit=$? output=$OUT/gitleaks-dir.json log=$OUT/gitleaks-dir.log"
else
  note "SKIP gitleaks: 'gitleaks' not found"
fi

if have trufflehog; then
  run_capture "trufflehog filesystem no verification" "$OUT/trufflehog-filesystem.jsonl" trufflehog filesystem --no-update --json --no-verification "$REPO"
else
  note "SKIP trufflehog: 'trufflehog' not found"
fi

if have go; then
  run_capture "go test all" "$OUT/go-test.txt" go test ./...
else
  note "SKIP go test: 'go' not found"
fi

run_json_report "govulncheck" govulncheck "$OUT/govulncheck.json" govulncheck -json ./...
run_json_report "gosec" gosec "$OUT/gosec.log" gosec -fmt=json -out "$OUT/gosec.json" ./...

if have semgrep; then
  export SEMGREP_SEND_METRICS=off
  run_capture "semgrep ${B11K_SEMGREP_CONFIG:-auto}" "$OUT/semgrep.log" semgrep scan --config "${B11K_SEMGREP_CONFIG:-auto}" --json --output "$OUT/semgrep.json"
else
  note "SKIP semgrep: 'semgrep' not found"
fi

if have osv-scanner; then
  run_capture "osv-scanner recursive" "$OUT/osv-scanner.json" osv-scanner -r --format json "$REPO"
else
  note "SKIP osv-scanner: 'osv-scanner' not found"
fi

if have trivy; then
  TRIVY_SCANNERS="${B11K_TRIVY_SCANNERS:-vuln,misconfig}"
  run_capture "trivy fs $TRIVY_SCANNERS" "$OUT/trivy.log" trivy fs --scanners "$TRIVY_SCANNERS" --format json --output "$OUT/trivy-fs.json" "$REPO"
else
  note "SKIP trivy: 'trivy' not found"
fi

if have mobsfscan; then
  IOS_PATH="$REPO/iosApp/B11k"
  if [ -d "$IOS_PATH" ]; then
    run_capture "mobsfscan ios source" "$OUT/mobsfscan-ios.log" mobsfscan --type ios --json -o "$OUT/mobsfscan-ios.json" "$IOS_PATH"
  else
    note "SKIP mobsfscan: iosApp/B11k not found"
  fi
else
  note "SKIP mobsfscan: 'mobsfscan' not found"
fi

if [ "${B11K_AUDIT_XCODE_ANALYZE:-0}" = "1" ]; then
  if have xcodebuild; then
    run_capture "xcodebuild analyze" "$OUT/xcodebuild-analyze.log" xcodebuild -project "$REPO/iosApp/B11k/B11k.xcodeproj" -scheme B11k -destination "generic/platform=iOS Simulator" -derivedDataPath "$OUT/xcode-derived" CODE_SIGNING_ALLOWED=NO analyze
  else
    note "SKIP xcodebuild analyze: 'xcodebuild' not found"
  fi
else
  note "SKIP xcodebuild analyze: set B11K_AUDIT_XCODE_ANALYZE=1"
fi

if [ -n "${B11K_IPA_PATH:-}" ]; then
  note "INFO MobSF IPA analysis requested for B11K_IPA_PATH=$B11K_IPA_PATH"
  note "INFO Upload/run MobSF manually or via your local MobSF API; avoid pasting API keys into chat."
else
  note "SKIP MobSF IPA analysis: set B11K_IPA_PATH=/path/to/B11k.ipa"
fi

if [ -n "${B11K_ZAP_TARGET:-}" ]; then
  if have docker; then
    note "RUN ZAP baseline against $B11K_ZAP_TARGET"
    docker run --rm -v "$OUT:/zap/wrk/:rw" -t ghcr.io/zaproxy/zaproxy:stable zap-baseline.py -t "$B11K_ZAP_TARGET" -J zap-baseline.json -r zap-baseline.html -I >"$OUT/zap-baseline.log" 2>&1
    note "  exit=$? output=$OUT/zap-baseline.json log=$OUT/zap-baseline.log"
  else
    note "SKIP ZAP baseline: 'docker' not found"
  fi
else
  note "SKIP ZAP baseline: set B11K_ZAP_TARGET=https://local-or-staging"
fi

note ""
note "Done. Start with $SUMMARY, then inspect JSON/log files. Keep secret values redacted in any user-facing summary."
