; A second (package orders ...) form: merge appends its items to the
; package from 10-orders.sexp.
(package orders
  (interface Pricer
    (doc "Pricer computes order totals.")
    (method Quote
      (doc "Quote returns the total for o in minor units.")
      (params (o *Order))
      (returns int64 error)))

  (llm "Write a Pricer that applies a 10% discount to orders over 100.00."))
