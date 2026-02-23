#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This installer supports Linux only." >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build subbox. Install Go first." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

PREFIX="${PREFIX:-/usr/local}"
BINDIR="${BINDIR:-${PREFIX}/bin}"
BIN_NAME="${BIN_NAME:-subbox}"
TARGET="${BINDIR}/${BIN_NAME}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

echo "Building ${BIN_NAME}..."
(
  cd "${REPO_DIR}"
  GO111MODULE=on go build -trimpath -ldflags="-s -w" -o "${TMP_DIR}/${BIN_NAME}" .
)

if [[ ! -d "${BINDIR}" ]]; then
  if [[ -w "$(dirname "${BINDIR}")" ]]; then
    mkdir -p "${BINDIR}"
  elif command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "${BINDIR}"
  else
    echo "Cannot create ${BINDIR}: no permissions and sudo is not available." >&2
    exit 1
  fi
fi

if [[ -w "${BINDIR}" ]]; then
  install -m 0755 "${TMP_DIR}/${BIN_NAME}" "${TARGET}"
elif command -v sudo >/dev/null 2>&1; then
  sudo install -m 0755 "${TMP_DIR}/${BIN_NAME}" "${TARGET}"
else
  echo "Cannot install to ${TARGET}: no permissions and sudo is not available." >&2
  exit 1
fi

echo "Installed: ${TARGET}"
echo "Run: ${BIN_NAME} -h"
