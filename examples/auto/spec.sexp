; Selection: the spec says what each store needs; tilegen picks the
; cheapest legal backend per store. Run `tilegen explain examples/auto`.
(project shop
  (module github.com/acme/shop)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))

  (package orders
    (entity Order
      (field ID uuid.UUID)
      (field TotalCents int64)
      (field PlacedAt time.Time)
      (store get list save
        (durable))))                  ; must survive a restart

  (package sessions
    (entity Session
      (field ID string)
      (field UserEmail string)
      (field ExpiresAt time.Time)
      (store get save delete))))      ; scratch data: may live in memory

(config
  (storage auto))                     ; the default; shown for clarity
