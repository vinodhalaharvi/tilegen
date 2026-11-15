package tiles

import "github.com/vinodhalaharvi/tilegen/internal/compile"

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "connect",
		Engine:     compile.NoEngine,
		Capability: "api",
		Doc:        "a .proto and a Connect server: Connect, gRPC and gRPC-Web on net/http",
		Requires:   []string{"buf", "protoc-gen-go", "protoc-gen-connect-go"},
		Cost: append(append(append(compile.Cost{}, apiHoles...), protoSchema...),
			compile.CostTerm{Dim: "dependency", Value: 2, Source: "derived", Note: "connect and protobuf"}),
		IllegalFor: map[string]string{
			"grpc-native": "Connect speaks the gRPC protocol but is not grpc-go: its interceptors, reflection and health services do not plug in",
		},
		Satisfies: map[string]string{
			"typed-clients":   "clients are generated from the .proto, in any language buf has a plugin for",
			"streaming":       "a server-streaming RPC, over the Connect, gRPC and gRPC-Web protocols alike",
			"browser-clients": "the Connect protocol is plain HTTP with JSON or binary bodies, so a browser calls it with no proxy",
		},
		Impl: &compile.APITransport{Emit: emitProtoAPI, Flavor: "connect", Plugin: "protoc-gen-connect-go",
			Imports: map[string]string{"connect": "connectrpc.com/connect"}},
	})
}
