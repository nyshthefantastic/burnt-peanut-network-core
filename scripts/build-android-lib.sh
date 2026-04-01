#!/usr/bin/env bash
# Cross-compile libcore.so for Android (arm64 + x86_64 emulator).
# Requires: Go with CGO, ANDROID_NDK (r21+), Android LLVM toolchain.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
: "${ANDROID_NDK:?Set ANDROID_NDK to the Android NDK root (e.g. $HOME/Library/Android/sdk/ndk/26.1.10909125)}"
ANDROID_NDK="${ANDROID_NDK%/}"

default_android_sdk() {
  case "$(uname -s)" in
  Darwin*) echo "${HOME}/Library/Android/sdk" ;;
  Linux*) echo "${ANDROID_SDK_ROOT:-${HOME}/Android/Sdk}" ;;
  *) echo "${ANDROID_SDK_ROOT:-${HOME}/Library/Android/sdk}" ;;
  esac
}

ndk_diagnose() {
  local root="$1"
  echo "── Listing: $root" >&2
  ls -la "$root" 1>&2 || echo "(path missing)" >&2
  if [[ -d "$root/toolchains/llvm" ]]; then
    echo "── Listing: $root/toolchains/llvm" >&2
    ls -la "$root/toolchains/llvm" 1>&2
  fi
}

# A full side-by-side NDK always has toolchains/llvm/prebuilt/<host>/bin/*-clang.
# Incomplete installs (or wrong path) omit prebuilt/; pick another version under the same SDK.
resolve_ndk_root() {
  local want="$1"
  if [[ -d "$want/toolchains/llvm/prebuilt" ]]; then
    echo "$want"
    return 0
  fi

  echo "warning: NDK at ANDROID_NDK has no toolchains/llvm/prebuilt (incomplete or wrong path):" >&2
  echo "  $want" >&2

  local sdk="${ANDROID_SDK_ROOT:-}"
  [[ -n "$sdk" && -d "$sdk/ndk" ]] || sdk="${ANDROID_HOME:-}"
  [[ -n "$sdk" && -d "$sdk/ndk" ]] || sdk="$(default_android_sdk)"

  if [[ ! -d "$sdk/ndk" ]]; then
    echo "error: No Android SDK ndk/ folder to search (tried ANDROID_SDK_ROOT, ANDROID_HOME, $(default_android_sdk))." >&2
    ndk_diagnose "$want"
    echo "Install or repair: Android Studio → Settings → Android SDK → SDK Tools → NDK (Side by side)." >&2
    return 1
  fi

  local ver full
  while IFS= read -r ver; do
    [[ -z "$ver" ]] && continue
    full="${sdk}/ndk/${ver}"
    if [[ -d "$full/toolchains/llvm/prebuilt" ]]; then
      echo "Using NDK instead: $full (from sdk/ndk scan)" >&2
      echo "$full"
      return 0
    fi
  done < <(ls -1 "$sdk/ndk" 2>/dev/null | sort -Vr)

  echo "error: No complete NDK under $sdk/ndk (none contain toolchains/llvm/prebuilt)." >&2
  echo "── Installed ndk versions:" >&2
  ls -la "$sdk/ndk" >&2 || true
  ndk_diagnose "$want"
  echo "Remove broken versions or reinstall NDK in SDK Manager, then retry." >&2
  return 1
}

ANDROID_NDK="$(resolve_ndk_root "$ANDROID_NDK")" || exit 1

# NDK r21–r26: prebuilt/darwin-arm64, prebuilt/linux-x86_64, etc.
# NDK r27+: the same names often work, but some installs use only darwin-x86_64 (Rosetta)
# or a single nested folder under prebuilt/. Discover by looking for Android clang binaries.
resolve_ndk_toolchain_bin() {
  local pre="$1/toolchains/llvm/prebuilt"
  if [[ ! -d "$pre" ]]; then
    echo "error: missing NDK LLVM prebuilt dir: $pre" >&2
    return 1
  fi
  local candidates=()
  case "$(uname -s)" in
  Darwin*)
    case "$(uname -m)" in
    arm64)
      candidates+=("$pre/darwin-arm64" "$pre/darwin-aarch64" "$pre/darwin-x86_64")
      ;;
    *)
      candidates+=("$pre/darwin-x86_64" "$pre/darwin-arm64")
      ;;
    esac
    ;;
  Linux*) candidates+=("$pre/linux-x86_64") ;;
  MINGW*|MSYS*|CYGWIN*) candidates+=("$pre/windows-x86_64") ;;
  *) candidates+=("$pre/linux-x86_64") ;;
  esac
  shopt -s nullglob
  local d arm x86 a x
  for d in "${candidates[@]}"; do
    a=("$d"/bin/aarch64-linux-android*-clang)
    x=("$d"/bin/x86_64-linux-android*-clang)
    arm="${a[0]-}"
    x86="${x[0]-}"
    if [[ -n "$arm" && -n "$x86" && -x "$arm" && -x "$x86" ]]; then
      shopt -u nullglob
      echo "$d/bin"
      return 0
    fi
  done
  for d in "$pre"/*; do
    [[ -d "$d/bin" ]] || continue
    a=("$d"/bin/aarch64-linux-android*-clang)
    x=("$d"/bin/x86_64-linux-android*-clang)
    arm="${a[0]-}"
    x86="${x[0]-}"
    if [[ -n "$arm" && -n "$x86" && -x "$arm" && -x "$x86" ]]; then
      shopt -u nullglob
      echo "$d/bin"
      return 0
    fi
  done
  shopt -u nullglob
  echo "error: could not find aarch64 + x86_64 Android clang under: $pre" >&2
  echo "Contents of prebuilt/:" >&2
  ls -la "$pre" >&2 || true
  return 1
}

TOOLCHAIN_BIN="$(resolve_ndk_toolchain_bin "$ANDROID_NDK")" || exit 1
shopt -s nullglob
arm_bins=("${TOOLCHAIN_BIN}"/aarch64-linux-android*-clang)
x86_bins=("${TOOLCHAIN_BIN}"/x86_64-linux-android*-clang)
CC_ARM64="${arm_bins[0]-}"
CC_X86="${x86_bins[0]-}"
shopt -u nullglob
if [[ ! -x "$CC_ARM64" || ! -x "$CC_X86" ]]; then
  echo "error: expected clang pair under ${TOOLCHAIN_BIN}" >&2
  exit 1
fi
echo "Using NDK toolchain: ${TOOLCHAIN_BIN}"
echo "  CC arm64: $CC_ARM64"
echo "  CC x86_64: $CC_X86"

# Android 16KB page-size compatibility for shared libraries.
PAGE_ALIGN_LDFLAGS="-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384"

out_arm="${ROOT}/build/android/arm64-v8a"
out_x86="${ROOT}/build/android/x86_64"
mkdir -p "$out_arm" "$out_x86"

echo "==> arm64-v8a"
CGO_ENABLED=1 GOOS=android GOARCH=arm64 CC="$CC_ARM64" CGO_LDFLAGS="$PAGE_ALIGN_LDFLAGS" \
  go build -buildmode=c-shared -trimpath \
  -o "${out_arm}/libcore.so" "${ROOT}/cabi"

echo "==> x86_64 (emulator)"
CGO_ENABLED=1 GOOS=android GOARCH=amd64 CC="$CC_X86" CGO_LDFLAGS="$PAGE_ALIGN_LDFLAGS" \
  go build -buildmode=c-shared -trimpath \
  -o "${out_x86}/libcore.so" "${ROOT}/cabi"

echo "Built:"
echo "  ${out_arm}/libcore.so"
echo "  ${out_x86}/libcore.so"
echo "Next: $(dirname "$0")/sync-android-jnilibs.sh"
