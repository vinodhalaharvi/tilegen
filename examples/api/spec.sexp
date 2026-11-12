; An order service whose API is chosen by what its clients need.
;
;   tilegen explain examples/api/spec.sexp   why Connect, and what else was legal
;   tilegen -out out/api examples/api/spec.sexp
;   cd out/api && buf generate && go mod tidy && go build ./...
;
; (typed-clients) rules out plain HTTP and JSON, which has no schema to
; generate a client from. Connect and gRPC both remain, and Connect is the
; cheaper of the two. Add (grpc-native) and only gRPC is left; replace
; both with (browser-clients) and gRPC is the one ruled out.
(project orders
  (module github.com/acme/orders)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))

  (package orders
    (doc "Package orders takes and tracks customer orders.")

    (enum Status pending paid shipped)

    (entity Order
      (field ID uuid.UUID)
      (field CustomerEmail string)
      (field TotalCents int64)
      (field Quantity int)
      (field Status Status)
      (field Note *string)
      (field PlacedAt time.Time)
      (field ShippedAt *time.Time)
      (store get list save delete))

    (enum Tier standard gold)

    ; A second entity, with a field of every other kind the wire carries.
    (entity Customer
      (field ID string)
      (field Email string)
      (field Tier *Tier)
      (field ReferredBy *uuid.UUID)
      (field Visits *int)
      (field Credit float64)
      (field Avatar []byte)
      (field Active bool)
      (store get save))

    (api
      (get Order) (list Order) (save Order) (delete Order)
      (get Customer) (save Customer)
      (typed-clients))))
