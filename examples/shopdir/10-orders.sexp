(package orders
  (doc "Package orders tracks customer orders.")

  (enum Status
    (doc "Status is where an order is in its lifecycle.")
    pending paid shipped cancelled)

  (entity Order
    (doc "Order is a placed customer order.")
    (field ID uuid.UUID)
    (field CustomerEmail string)
    (field TotalCents int64 (doc "Total in minor currency units."))
    (field PlacedAt time.Time)
    (field ShippedAt *time.Time)
    (field Status Status)
    (store get list save delete
      (list-by CustomerEmail)
      count
      (constraint "Save must be idempotent for the same ID.")
      (constraint "Never log CustomerEmail."))))
