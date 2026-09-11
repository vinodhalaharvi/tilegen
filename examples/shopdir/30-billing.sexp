(package billing
  (doc "Package billing invoices orders.")

  (entity Invoice
    (doc "Invoice bills one order.")
    (field ID uuid.UUID)
    (field OrderID uuid.UUID)
    (field AmountCents int64)
    (field IssuedAt time.Time)
    (store get save))

  (interface Invoicer
    (doc "Invoicer turns orders into invoices.")
    (method InvoiceFor
      (doc "InvoiceFor builds the invoice for o, priced by p.")
      (params (o *orders.Order) (p orders.Pricer))
      (returns *Invoice error))))
