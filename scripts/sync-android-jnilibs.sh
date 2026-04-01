#!/usr/bin/env bash
# Copy Go-built libcore.so into the Android app jniLibs tree.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
JNI="${ROOT}/android/app/src/main/jniLibs"
for pair in "arm64-v8a" "x86_64"; do
  src="${ROOT}/build/android/${pair}/libcore.so"
  if [[ ! -f "$src" ]]; then
    echo "error: missing $src — run scripts/build-android-lib.sh first" >&2
    exit 1
  fi
  mkdir -p "${JNI}/${pair}"
  cp -f "$src" "${JNI}/${pair}/libcore.so"
  echo "copied -> ${JNI}/${pair}/libcore.so"
done
