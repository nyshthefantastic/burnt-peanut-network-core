.PHONY: proto clean build-android-lib build-android-arm64 build-android-x86 build-ios test ci

# Android NDK: set ANDROID_NDK to the NDK root. Host prebuilt dir is detected automatically.
ANDROID_HOST_PREBUILT := $(shell case "$$(uname -s)" in Darwin*) case "$$(uname -m)" in arm64) echo darwin-arm64;; *) echo darwin-x86_64;; esac ;; MINGW*|MSYS*) echo windows-x86_64 ;; *) echo linux-x86_64 ;; esac)
ANDROID_CC_ARM64 ?= $(ANDROID_NDK)/toolchains/llvm/prebuilt/$(ANDROID_HOST_PREBUILT)/bin/aarch64-linux-android21-clang
ANDROID_CC_X86  ?= $(ANDROID_NDK)/toolchains/llvm/prebuilt/$(ANDROID_HOST_PREBUILT)/bin/x86_64-linux-android21-clang

proto:
	@mkdir -p wire/gen
	protoc \
		--proto_path=wire/proto \
		--go_out=wire/gen \
		--go_opt=paths=source_relative \
		wire/proto/core.proto

# ─── Android ───

# Preferred: ./scripts/build-android-lib.sh (same outputs + clearer errors)
build-android-lib:
	./scripts/build-android-lib.sh

build-android-arm64:
	CGO_ENABLED=1 \
	GOOS=android \
	GOARCH=arm64 \
	CC=$(ANDROID_CC_ARM64) \
	go build -buildmode=c-shared -trimpath \
		-o build/android/arm64-v8a/libcore.so \
		./cabi/

build-android-x86:
	CGO_ENABLED=1 \
	GOOS=android \
	GOARCH=amd64 \
	CC=$(ANDROID_CC_X86) \
	go build -buildmode=c-shared -trimpath \
		-o build/android/x86_64/libcore.so \
		./cabi/

# ─── iOS ───

build-ios:
	CGO_ENABLED=1 \
	GOOS=ios \
	GOARCH=arm64 \
	CC=$(shell xcrun --sdk iphoneos --find clang) \
	CGO_CFLAGS="-isysroot $(shell xcrun --sdk iphoneos --show-sdk-path) -arch arm64" \
	go build -buildmode=c-archive \
		-o build/ios/libcore.a \
		./cabi/

# ─── All ───

build-all: build-android-lib build-ios

test:
	go test ./...

ci:
	./scripts/ci.sh

clean:
	rm -rf wire/gen/*.pb.go
	rm -rf build/