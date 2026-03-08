.PHONY: proto clean build-android-arm64 build-android-x86 build-ios

proto:
	@mkdir -p wire/gen
	protoc \
		--proto_path=wire/proto \
		--go_out=wire/gen \
		--go_opt=paths=source_relative \
		wire/proto/core.proto

# ─── Android ───

build-android-arm64:
	CGO_ENABLED=1 \
	GOOS=android \
	GOARCH=arm64 \
	CC=$(ANDROID_NDK)/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android21-clang \
	go build -buildmode=c-shared \
		-o build/android/arm64/libcore.so \
		./cabi/

build-android-x86:
	CGO_ENABLED=1 \
	GOOS=android \
	GOARCH=amd64 \
	CC=$(ANDROID_NDK)/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android21-clang \
	go build -buildmode=c-shared \
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

build-all: build-android-arm64 build-android-x86 build-ios

clean:
	rm -rf wire/gen/*.pb.go
	rm -rf build/