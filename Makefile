.PHONY: proto clean

proto:
	@mkdir -p wire/gen
	protoc \
		--proto_path=wire/proto \
		--go_out=wire/gen \
		--go_opt=paths=source_relative \
		wire/proto/meshledger.proto

clean:
	rm -rf wire/gen/*.pb.go