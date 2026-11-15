// Package tiles holds tilegen's built-in tiles. Importing it registers them.
//
// A tile is one file, named for what it offers and what it is:
// store_bolt.go, bus_kafka.go, api_grpc.go, auth_oidc.go, secrets_vault.go. The file registers the tile
// (what it guarantees, what rules it out, what it costs) and holds the
// code that emits its Go. Adding a tile is adding a file; removing one is
// deleting it.
//
// # Stores
//
// Each storage tile implements one store for an entity: the struct, its
// constructor, and a hole per method.
//
// # Event buses
//
// Each bus declares what it cannot do, and those clauses are the point: a
// spec that says (replay) rules out the queue, a spec that says
// (no-broker) rules out every remote bus, and a reader can check every
// sentence against the system it describes. The implementations are
// thin: a transport's generated code is a struct, a constructor and two
// holes. What the tile is for is the legality and the price.
//
// NATS is two tiles, because it is two systems that ship in one binary.
// Core NATS drops a message with no connected subscriber; JetStream keeps
// it. They hold different types, which is what makes a claim reachable
// from the hole rather than asserted beside it: nats-core gets a
// *nats.Conn, nats-jetstream gets a jetstream.JetStream. A durability
// claim next to a bare *nats.Conn is one a reviewer can object to.
//
// A bus that is also a way in from outside registers twice in its one
// file, once for each job: bus_kafka.go and bus_nats_core.go do.
//
// # APIs
//
// http-json writes handlers by hand; connect and grpc share a .proto and
// the code in api_proto.go that builds it. Each leaves two things open:
// what makes a value acceptable, and which code an error deserves.
//
// # Secrets
//
// Three places a secret can come from. Nothing in them is judgment, so
// they leave no holes.
//
// # Auth
//
// The four ways to decide who a request is from differ on four questions
// a spec can ask, and the answers are properties of the scheme rather
// than of any library:
//
//	              revocable  third-party-idp  offline-verify  expiring
//	jwt-local         no           no              yes           yes
//	oidc              yes          yes             yes           yes
//	session-store     yes          no              no            yes
//	api-key           yes          no              no            no
//
// oidc is legal for everything and expensive to run, which is the shape
// the vocabulary should produce: the most capable answer wins only when
// something is actually asked for.
package tiles
