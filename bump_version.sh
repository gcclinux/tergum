#!/bin/bash
# Bump version across all source files
# Usage: ./bump_version.sh <version>
# Example: ./bump_version.sh 1.2.0

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 1.2.0"
    exit 1
fi

NEW_VERSION="$1"

# Validate version format (semver: major.minor.patch with optional pre-release)
if ! echo "$NEW_VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$'; then
    echo "Error: Invalid version format: '$NEW_VERSION'. Expected format: X.Y.Z or X.Y.Z-pre" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Files to update
RELEASE_FILE="$SCRIPT_DIR/release"
ROOT_VERSION_JSON="$SCRIPT_DIR/version.json"
INTERNAL_VERSION_JSON="$SCRIPT_DIR/internal/version/version.json"

# 1. Update release file
printf '%s' "$NEW_VERSION" > "$RELEASE_FILE"
echo "Updated: release -> $NEW_VERSION"

# 2. Update root version.json
cat > "$ROOT_VERSION_JSON" <<EOF
{
  "version": "$NEW_VERSION",
  "build": "dev"
}
EOF
echo "Updated: version.json -> $NEW_VERSION"

# 3. Update internal/version/version.json (must match root exactly)
cp "$ROOT_VERSION_JSON" "$INTERNAL_VERSION_JSON"
echo "Updated: internal/version/version.json -> $NEW_VERSION"

echo ""
echo "Version bumped to $NEW_VERSION across all files."
echo "Run ./build.sh to build with the new version."
