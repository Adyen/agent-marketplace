## 1. Cursor CLI inventory

- [x] 1.1 Parse Cursor CLI rows only after an `Available models` header
- [x] 1.2 Test that diagnostic lines without a header report `source.inventory-unparsed`
- [x] 1.3 Test that diagnostic lines before the header are ignored

## 2. Cursor documentation failure

- [x] 2.1 Keep a valid CLI inventory when the Cursor documentation fails to parse
- [x] 2.2 Test that `source.parse-failed` is reported and CLI identifiers are compared with `cursor.yaml`

## 3. Gemini API documentation

- [x] 3.1 Extract backtick-only continuation lines of a bullet item
- [x] 3.2 Extend the Gemini fixture test and update the `gemini-api-models` baseline digest
