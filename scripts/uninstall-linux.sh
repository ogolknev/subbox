#!/usr/bin/env bash
set -euo pipefail

PREFIX="${PREFIX:-/usr/local}"
BINDIR="${BINDIR:-${PREFIX}/bin}"
BIN_NAME="${BIN_NAME:-subbox}"
TARGET="${BINDIR}/${BIN_NAME}"

if [[ ! -e "${TARGET}" ]]; then
  echo "Nothing to remove: ${TARGET}"
  exit 0
fi

if [[ -w "${BINDIR}" ]]; then
  rm -f "${TARGET}"
elif command -v sudo >/dev/null 2>&1; then
  sudo rm -f "${TARGET}"
else
  echo "Cannot remove ${TARGET}: no permissions and sudo is not available." >&2
  exit 1
fi

echo "Removed: ${TARGET}"
