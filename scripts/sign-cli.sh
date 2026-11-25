#!/bin/sh
# Sign the GoReleaser-built CLI before it is archived, so the helper's caller
# check accepts it. No-ops for non-macOS targets.
set -eu

target="$1"
path="$2"

case "$target" in
	darwin*) ;;
	*) exit 0 ;;
esac

identity="${SIGN_IDENTITY:--}"
if [ "$identity" = "-" ]; then
	# No Developer ID: ad-hoc signing still gives us the stable identifier.
	codesign --force --sign - \
		--identifier com.thebuildlabs.calendarctl \
		--options runtime "$path"
else
	codesign --force --sign "$identity" \
		--identifier com.thebuildlabs.calendarctl \
		--options runtime "$path"
fi
