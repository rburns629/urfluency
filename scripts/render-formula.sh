#!/bin/sh
# Renders the Homebrew formula from release archives:
# scripts/render-formula.sh VERSION ARCHIVE_DIR > urfluency.rb
set -eu
version=$1
dir=$2
sum() {
  f="$dir/urfluency_${version}_$1.tar.gz"
  [ -f "$f" ] || { echo "missing $f" >&2; exit 1; }
  shasum -a 256 "$f" | cut -d' ' -f1
}
# Plain assignments, so a missing archive stops the script under set -e; a
# command substitution inside sed's arguments would not.
darwin_arm64=$(sum darwin_arm64)
darwin_amd64=$(sum darwin_amd64)
linux_arm64=$(sum linux_arm64)
linux_amd64=$(sum linux_amd64)
sed -e "s/{{VERSION}}/$version/g" \
  -e "s/{{SHA256_DARWIN_ARM64}}/$darwin_arm64/" \
  -e "s/{{SHA256_DARWIN_AMD64}}/$darwin_amd64/" \
  -e "s/{{SHA256_LINUX_ARM64}}/$linux_arm64/" \
  -e "s/{{SHA256_LINUX_AMD64}}/$linux_amd64/" \
  "$(dirname "$0")/../packaging/homebrew/urfluency.rb.tmpl"
