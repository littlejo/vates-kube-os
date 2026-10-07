#!/bin/bash
# Run one component's recipe: lib/run-recipe.sh <name>
#
# Sources the shared environment, then the recipe that defines build() for the
# name, and calls it. One recipe per component, so a Containerfile RUN line per
# component gives one cache layer per component.
set -euo pipefail

name="${1:?usage: run-recipe.sh <name>}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
: "${SCRATCH:=$(dirname "${here}")}"

# shellcheck source=/dev/null
source "${here}/common.sh"

recipe=""
for f in "${SCRATCH}"/recipes/*/"${name}".sh "${SCRATCH}/recipes/${name}.sh"; do
	if [ -f "${f}" ]; then
		recipe="${f}"
		break
	fi
done
if [ -z "${recipe}" ]; then
	echo "run-recipe: no recipe for '${name}'" >&2
	exit 1
fi

echo "=== [$(basename "$(dirname "${recipe}")")] ${name} ==="
# shellcheck source=/dev/null
source "${recipe}"
build
