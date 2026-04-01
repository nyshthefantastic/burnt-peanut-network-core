# Native `libcore.so`

CMake links `mljni` against `libcore.so` per ABI. Before the first Gradle sync / build:

1. From the repo root, with `ANDROID_NDK` set:

   ```bash
   ./scripts/build-android-lib.sh
   ./scripts/sync-android-jnilibs.sh
   ```

2. You should have:

   - `jniLibs/arm64-v8a/libcore.so`
   - `jniLibs/x86_64/libcore.so`

These binaries are not committed (large, platform-specific).
