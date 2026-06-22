#!/bin/sh
# Apply synccontact patches to upstream Go module dependencies before building.
#
# bridgev2 (part of maunium.net/go/mautrix) is framework code that lives outside
# our own packages, so behavioural changes to it can't go in pkg/connector. We
# copy the pinned module out of the read-only module cache, apply the patches in
# this directory, and wire the copy in via a local `replace` directive. Keeping
# the change as a patch (rather than a vendored fork) keeps the diff reviewable
# and makes mautrix version bumps a re-apply rather than a re-vendor.
set -eu

patch_dir="$(cd "$(dirname "$0")" && pwd)"
module="maunium.net/go/mautrix"
dest="/build/.patched/mautrix"

cd /build
go mod download "$module"

src="$(go list -m -f '{{.Dir}}' "$module")"
rm -rf "$dest"
mkdir -p "$(dirname "$dest")"
cp -a "$src" "$dest"
chmod -R u+w "$dest"

for p in "$patch_dir"/*.patch; do
	[ -e "$p" ] || continue
	echo "Applying $(basename "$p") to $module"
	patch -p1 -d "$dest" <"$p"
done

go mod edit -replace "$module=$dest"
echo "Patched $module -> $dest"
