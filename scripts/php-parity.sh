#!/bin/sh
# Runs every fixture through both the Go binary and the PHP hook scripts and
# reports any disagreement in the allow/deny decision.
#
# usage: scripts/php-parity.sh <path-to-php-hook-repo>
#
# The PHP scripts are copied to a temp directory before use so that their
# log files land there and not in the repo. In the copies, any leading
# exit(0); line is removed and every 'enabled' => false is switched on.

set -eu

if [ $# -ne 1 ] || [ ! -f "$1/check.php" ]; then
    echo "usage: $0 <path-to-php-hook-repo>" >&2
    exit 64
fi
phpsrc=$1
root=$(git rev-parse --show-toplevel)
fixtures="$root/internal/aec/testdata/fixtures"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/php" "$tmp/config"

go build -o "$tmp/aec" "$root/cmd/aec"

for f in check.php tool-use.php em-dash-patterns.php; do
    awk '/^\$rules = \[/ { body = 1 } body || $0 != "exit(0);" { print }' "$phpsrc/$f" \
        | sed "s/'enabled' => false/'enabled' => true/" > "$tmp/php/$f"
done

decision() {
    if [ "$1" -eq 0 ]; then echo allow; else echo deny; fi
}

agree=0
skipped=0
disagree=0

for kind in edit bash; do
    if [ "$kind" = edit ]; then script=check.php; else script=tool-use.php; fi
    for file in "$fixtures/$kind"/*.json; do
        name="$kind/$(basename "$file" .json)"
        skip=$(jq -r '.skip_php // empty' "$file")
        if [ -n "$skip" ]; then
            echo "$name known divergence: $skip"
            skipped=$((skipped + 1))
            continue
        fi
        payload=$(jq -c .payload "$file")

        set +e
        printf '%s' "$payload" | php "$tmp/php/$script" >/dev/null 2>&1
        php_code=$?
        printf '%s' "$payload" | XDG_CONFIG_HOME="$tmp/config" "$tmp/aec" hook "$kind" >/dev/null 2>&1
        go_code=$?
        set -e

        if [ "$php_code" -eq "$go_code" ]; then
            agree=$((agree + 1))
        else
            echo "$name php=$(decision "$php_code") go=$(decision "$go_code")"
            disagree=$((disagree + 1))
        fi
    done
done

if [ "$disagree" -ne 0 ]; then
    echo "$disagree disagreements" >&2
    exit 1
fi
echo "$agree fixtures agree, $skipped known divergences"
