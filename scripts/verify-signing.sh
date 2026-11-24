#!/bin/sh
# Release guard: fail closed unless the built CLI and helper are signed with
# the Developer ID Team ID pinned in the helper's Info.plist.
#
# Usage: verify-signing.sh <cli-binary> <helper-binary> <helper-Info.plist>
set -eu

CLI=${1:-bin/calendarctl}
HELPER=${2:-bin/EventKitBridge.app/Contents/MacOS/EventKitBridge}
PLIST=${3:-bin/EventKitBridge.app/Contents/Info.plist}

die() {
	echo "verify-signing: $*" >&2
	exit 1
}

[ "$(uname -s)" = "Darwin" ] || die "macOS is required to verify code signatures"
command -v codesign >/dev/null 2>&1 || die "codesign not found in PATH"
command -v plutil >/dev/null 2>&1 || die "plutil not found in PATH"
for file in "$CLI" "$HELPER" "$PLIST"; do
	[ -f "$file" ] || die "missing $file"
done

# The plist value is the anchor. An empty value means this is an ad-hoc local
# build, which is not acceptable for a release artifact.
team=$(plutil -extract CalendarctlTrustedTeamID raw -o - "$PLIST" | tr -d '\r\n[:space:]')
[ -n "$team" ] || die "helper has an empty CalendarctlTrustedTeamID; release builds must pin a Developer ID Team ID"

verify_one() {
	file=$1
	identifier=$2
	info=$(codesign -dvv "$file" 2>&1) || die "$file is not signed: $info"
	actual=$(printf '%s\n' "$info" | sed -n 's/^TeamIdentifier=//p' | head -n 1)
	[ -n "$actual" ] && [ "$actual" != "not set" ] || die "$file has no Developer ID Team ID"
	[ "$actual" = "$team" ] || die "$file Team ID '$actual' does not match pinned '$team'"
	printf '%s\n' "$info" | grep -qx "Identifier=$identifier" || die "$file has unexpected signing identifier"
	codesign --verify --strict --verbose=0 "$file" || die "signature invalid for $file"
}

verify_one "$CLI" com.thebuildlabs.calendarctl
verify_one "$HELPER" com.thebuildlabs.calendarctl.bridge

echo "verify-signing: OK (Team ID $team)"
