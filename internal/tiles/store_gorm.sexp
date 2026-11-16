(tile gorm
  (offers store)
  (doc "GORM: the entity as a model, on whatever database the caller opened")
  (import gorm gorm.io/gorm)
  (error-package gorm)
  (topics gorm orm)

  (satisfies (lookup-by-field)
    "a WHERE on the field's column, and an index when the model declares one")
  (unverified (durable)
    "it depends on the database the *gorm.DB was opened on, which is the caller's")
  (unverified (cross-process)
    "it depends on the database the *gorm.DB was opened on, which is the caller's")
  (unverified (no-broker)
    "it depends on the database the *gorm.DB was opened on, which is the caller's")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; db.Transaction can hold both, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; db.Transaction can hold both, and this tile does not generate it")

  (cost
    (llm-work 3 (source derived "one call per method, and the errors to map"))
    (maintenance 4 (source derived "the table follows the struct by convention: a renamed field is a renamed column that AutoMigrate adds beside the old one, and what a query does is decided at run time"))
    (dependency 3 (source derived "GORM, and the driver the caller chooses"))
    (runtime 2 (source derived "reflection on every call, over the driver"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *gorm.DB))
  (new (params (db *gorm.DB))
    "return &{impl}{db: db}")

  (hint get
    "Declare var v {entity} and call s.db.WithContext(ctx).First(&v, \"id = ?\", the ID).Error. When errors.Is(err, gorm.ErrRecordNotFound), return ErrNotFound. The table is the caller's to create, with s.db.AutoMigrate(&{entity}{}); GORM takes the ID field as the primary key.")

  (hint list
    "Declare a slice of {entity} and call s.db.WithContext(ctx).Order(\"id\").Find(&values).Error. No rows is an empty slice, not an error.")

  (hint save
    "Call s.db.WithContext(ctx).Save(the value).Error. With the primary key set, Save updates the row and inserts it when there is none.")

  (hint delete
    "Call s.db.WithContext(ctx).Delete(&{entity}{}, \"id = ?\", the ID).Error. Deleting a missing row is not an error. The condition matters: Delete with no condition is refused by GORM.")

  (hint count
    "Declare var n int64 and call s.db.WithContext(ctx).Model(&{entity}{}).Count(&n).Error.")

  (hint list-by
    "Call s.db.WithContext(ctx).Where(\"COLUMN = ?\", {param}).Order(\"id\").Find(&values).Error, where COLUMN is {field} as GORM names it: snake_case, so SellerID is seller_id.")

  (hint get-by
    "Call s.db.WithContext(ctx).Where(\"COLUMN = ?\", {param}).First(&v).Error, where COLUMN is {field} in snake_case. When errors.Is(err, gorm.ErrRecordNotFound), return ErrNotFound.")

  (hint count-by
    "Call s.db.WithContext(ctx).Model(&{entity}{}).Where(\"COLUMN = ?\", {param}).Count(&n).Error, where COLUMN is {field} in snake_case.")

  (hint exists-by
    "Count as CountBy would, with the same Where on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.db.WithContext(ctx).Where(\"COLUMN = ?\", {param}).Delete(&{entity}{}).Error, where COLUMN is {field} in snake_case."))
