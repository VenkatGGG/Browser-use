# Proto contracts

`node.proto` defines internal orchestrator <-> node-agent RPC contracts.

Generate Go stubs with:

```bash
./scripts/generate_proto.sh
```

Requirements:
- `protoc`
- `protoc-gen-go`
- `protoc-gen-go-grpc`

Example setup on macOS:

```bash
brew install protobuf
GOBIN=/opt/homebrew/bin go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
GOBIN=/opt/homebrew/bin go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```
