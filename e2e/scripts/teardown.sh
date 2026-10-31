#!/usr/bin/env bash
# Safety net after a run: removes the e2e sandbox even if serve.sh was killed.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
VISOR_SANDBOX_DIR=/tmp/visor-e2e "$ROOT/scripts/sandbox.sh" down
