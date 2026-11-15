package tiles

import "github.com/vinodhalaharvi/tilegen/internal/compile"

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "grpc",
		Engine:     compile.NoEngine,
		Capability: "api",
		Doc:        "a .proto and a grpc-go server",
		Requires:   []string{"buf", "protoc-gen-go", "protoc-gen-go-grpc"},
		Cost: append(append(append(compile.Cost{}, apiHoles...), protoSchema...),
			compile.CostTerm{Dim: "dependency", Value: 3, Source: "derived", Note: "grpc-go and protobuf: a large module tree"}),
		IllegalFor: map[string]string{
			"browser-clients": "a browser cannot speak gRPC; it needs gRPC-Web and a proxy in front of the server",
		},
		Satisfies: map[string]string{
			"typed-clients": "clients are generated from the .proto, in any language protoc has a plugin for",
			"streaming":     "a server-streaming RPC over HTTP/2",
			"grpc-native":   "it is grpc-go: interceptors, reflection, health and load balancing work as documented",
		},
		Impl: &compile.APITransport{Emit: emitProtoAPI, Flavor: "grpc", Plugin: "protoc-gen-go-grpc",
			Imports: map[string]string{"codes": "google.golang.org/grpc/codes"}},
	})
}
