package tiles

import "github.com/vinodhalaharvi/tilegen/internal/compile"

// What the API tiles have in common.

// apiHoles is what every API tile leaves to be written.
var apiHoles = compile.Cost{{Dim: "llm-work", Value: 2, Source: "derived", Note: "only validation and error mapping are left open"}}

// protoSchema is what a tile with a .proto costs to keep.
var protoSchema = compile.Cost{{Dim: "maintenance", Value: 2, Source: "derived", Note: "a .proto and its generated code to keep in step, and field numbers that can never be reused"}}
