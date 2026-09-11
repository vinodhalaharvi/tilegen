(package orders
  (doc "Package orders tracks customer orders.")

  (entity Order
    (doc "Order is a placed customer order.")
    (field ID uuid.UUID)
    (field CustomerEmail string)
    (field TotalCents int64 (doc "Total in minor currency units."))
    (field PlacedAt time.Time)
    (field ShippedAt *time.Time)
    (store get list save delete
      (constraint "Save must be idempotent for the same ID.")
      (constraint "Never log CustomerEmail."))))
