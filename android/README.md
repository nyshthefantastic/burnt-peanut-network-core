# Android demo app

Minimal **Burnt Peanut Network Core** shell: loads `libcore.so` (Go `cabi`) plus a small JNI bridge (`mljni`) that supplies stub `MLCallbacks`, creates a node, then destroys it.

## Prerequisites

- [Android Studio](https://developer.android.com/studio) (Koala / recent AGP)
- **Android NDK** (SDK Manager → SDK Tools → NDK)
- **Go** with **CGO** (same version as `go.mod`), on your `PATH`

## One-time: build `libcore.so` and copy into the app

From the **repository root** (not `android/`):

```bash
export ANDROID_NDK="$HOME/Library/Android/sdk/ndk/<your-ndk-version>"   # macOS example
./scripts/build-android-lib.sh
./scripts/sync-android-jnilibs.sh
```

If the script says **`missing NDK LLVM prebuilt dir`** for the path you exported, that NDK revision is often **incomplete** (download failed or partial folder). The build script will **scan** `~/Library/Android/sdk/ndk/*` (or `ANDROID_SDK_ROOT/ndk/*`) and use the **newest complete** install. You can also remove the broken folder and reinstall **NDK (Side by side)** in **SDK Manager → SDK Tools**.

On Apple Silicon, some NDK versions only ship **`darwin-x86_64`** (Rosetta); the script tries that after `darwin-arm64`.

You must have `libcore.so` under `android/app/src/main/jniLibs/arm64-v8a/` and `.../x86_64/` before CMake can link `mljni`. See `jniLibs/README.md`.

## Open and run

1. In Android Studio: **File → Open** → select the `android/` directory.
2. If Gradle asks to create a wrapper or download a distribution, accept.
3. Create `android/local.properties` if missing (Studio usually does this):

   ```properties
   sdk.dir=/path/to/Android/sdk
   ```

4. Run the **app** configuration on an **arm64** device or **x86_64** emulator.

Tap **Smoke test node**. A non-zero handle means `ml_node_create` succeeded with stub callbacks.

## Next steps (production)

- Replace stub callbacks in `app/src/main/cpp/ml_jni.cpp` with JNI that forwards to Kotlin (BLE transport, chunk storage, secure element, notifications).
- Keep `cabi/core.h` as the single source of truth for the C API.

## Command-line build (optional)

If the Gradle wrapper is present:

```bash
cd android
./gradlew :app:assembleDebug
```

Install the APK from `app/build/outputs/apk/debug/`.
