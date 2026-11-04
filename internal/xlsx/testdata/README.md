# Workbooks for the xlsx tests

## Read

`excel.xlsx`, `libreoffice.xlsx` and `google.xlsx` are written by `make_fixtures.py`. Each has the
parts and cell types its program writes, but none was saved from that program. A workbook saved
from one of them can be put beside them, and the reader's first test reads it the same way.

## Written

`written.xlsx` is what the writer writes for `fixtureSheets` in `write_test.go`. The test fails
when the writer's output changes, so a change to the writer is checked again before the file is
replaced:

1. Open the new output in LibreOffice and check every value. Check that the formula-like text
   stays text, the dates are dates, and both sheets are there with their names.
2. Replace the file: `BAM_UPDATE_FIXTURES=1 go test ./internal/xlsx/`.

It was opened in LibreOffice 25.2.3, headless, converted to CSV and HTML. Every value read as
written, as numbers, ISO dates, `TRUE` and `FALSE`, and text. `=HYPERLINK(...)` stayed text, and
the sheet `Tools › Saws` kept its name. LibreOffice opening it is the check: the format is `.xlsx`,
which Excel reads as well.
