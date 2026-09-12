; A second (package orders ...) form: merge appends its items to the
; package from 10-orders.sexp.
(package orders
  (interface Pricer
    (doc "Pricer computes order totals.")
    (method Quote
      (doc "Quote returns the total for o in minor units.")
      (params (o *Order))
      (returns int64 error)))

  ; HTTP: a Handler and ServeMux over the store; only validation and
  ; error mapping are left open.
  (http
    (route GET    "/orders"      (list Order))
    (route GET    "/orders/{id}" (get Order))
    (route POST   "/orders"      (save Order)))

  ; Events: typed structs, a Bus interface and an in-process LocalBus, no holes.
  (events
    (event OrderPlaced (field OrderID uuid.UUID) (field TotalCents int64))
    (event OrderShipped (field OrderID uuid.UUID)))

  (llm "Write a Pricer that applies a 10% discount to orders over 100.00."))
