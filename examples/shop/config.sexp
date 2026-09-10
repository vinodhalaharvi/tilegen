; House style. Same spec + different config = different, equally valid Go.
(config
  (json-tags snake)     ; snake | camel | none
  (context-first yes)   ; yes | no  - prepend ctx context.Context to methods
  (storage memory)      ; memory | postgres (postgres emits sqlc inputs)
  (layout flat))        ; flat | internal
