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
