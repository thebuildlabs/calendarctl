#!/bin/sh
# Import the release Developer ID certificate into an ephemeral keychain so
# `codesign` can use it non-interactively in GitHub Actions.
#
# Required environment (all GitHub Actions secrets):
#   SIGN_IDENTITY                  exact codesign identity, e.g.
#                                  "Developer ID Application: The Build Labs (TEAMID)"
#   SIGNING_CERTIFICATE_P12_BASE64 base64-encoded .p12 containing cert + private key
#   SIGNING_CERTIFICATE_PASSWORD   password protecting the .p12
# Optional:
#   KEYCHAIN_PASSWORD              password for the temporary keychain
set -eu

[ "$(uname -s)" = "Darwin" ] || { echo "import-signing-cert: macOS is required" >&2; exit 1; }
: "${SIGN_IDENTITY:?SIGN_IDENTITY secret is required}"
: "${SIGNING_CERTIFICATE_P12_BASE64:?SIGNING_CERTIFICATE_P12_BASE64 secret is required}"
: "${SIGNING_CERTIFICATE_PASSWORD:?SIGNING_CERTIFICATE_PASSWORD secret is required}"

work="${RUNNER_TEMP:-$(mktemp -d)}"
keychain="$work/calendarctl-signing.keychain-db"
p12="$work/calendarctl-signing.p12"
password="${KEYCHAIN_PASSWORD:-calendarctl-ci-signing}"

security create-keychain -p "$password" "$keychain"
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$password" "$keychain"

printf '%s' "$SIGNING_CERTIFICATE_P12_BASE64" | base64 --decode > "$p12"
# Only codesign may use the imported private key.
security import "$p12" -k "$keychain" -P "$SIGNING_CERTIFICATE_PASSWORD" \
	-T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$password" "$keychain"
# shellcheck disable=SC2046
security list-keychains -d user -s "$keychain" $(security list-keychains -d user | sed 's/"//g')

if ! security find-identity -v -p codesigning "$keychain" | grep -F "$SIGN_IDENTITY" >/dev/null; then
	echo "import-signing-cert: SIGN_IDENTITY was not found in the imported certificate" >&2
	exit 1
fi

# Developer ID identities end with the 10-character Team ID in parentheses.
# Export it so the Go binaries can pin the helper to this Team ID as well.
team_id=$(printf '%s' "$SIGN_IDENTITY" | sed -n 's/.*(\([A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9]\)).*/\1/p')
[ -n "$team_id" ] || {
	echo "import-signing-cert: cannot determine Team ID from SIGN_IDENTITY" >&2
	exit 1
}

rm -f "$p12"
# Make values available to later steps (including cleanup and GoReleaser).
echo "KEYCHAIN_PATH=$keychain" >> "$GITHUB_ENV"
echo "TEAM_ID=$team_id" >> "$GITHUB_ENV"
echo "import-signing-cert: ready to sign as $SIGN_IDENTITY"
