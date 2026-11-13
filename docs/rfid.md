# RFID tags

Long-range RFID tags (UHF, EPC Class 1 Gen 2, sold as RAIN RFID) are read from meters away, many at
once, with no line of sight. BAM! writes them while printing labels, and finds assets by them when
a reader types what it reads.

This is untested on RFID hardware. It follows Zebra's ZPL programming guide and GS1's published
encoding examples; see [Sources](#sources). Short-range NFC tags, which phones read, are
another kind, and these settings don't touch them.

## What a tag holds

A tag holds an EPC, a number of 16-bit words. BAM! writes one of each asset's codes to it, under a
scheme the instance chooses in **Admin → Printers → RFID tags**, with `settings.manage`:

| Scheme | Key | Writes | Needs |
| --- | --- | --- | --- |
| Text (the default) | `text` | The code's own characters, one byte each, with a zero byte after an odd number to fill the last word. `HS-105` is `48532D313035`. Up to 62 printable ASCII characters. | Nothing. Twelve characters fill the 96 bits most tags hold. |
| GIAI-96 | `giai-96` | GS1's Global Individual Asset Identifier in 96 bits. | A GS1 company prefix. Codes must be whole numbers with no leading zeros. |
| GIAI-202 | `giai-202` | GIAI in 202 bits, written as 208. | A GS1 company prefix, and tags of 208 bits or more. Codes of letters, digits and `!"%&'()*+,-./:;<=>?_`, up to 30 characters less the prefix's digits. |

GIAI is the standard for assets: a reader anywhere knows the tag for one of yours. It takes a GS1
company prefix, which GS1 sells. The text scheme is private. It suits an organization with no
prefix, and its tags read back as the codes themselves where a reader converts hex to text.

The other settings:

- **Company prefix:** 6 to 12 digits, for GIAI.
- **Filter:** 0 to 7, for GIAI. Readers can be set to read only tags of one filter value. 0 is
  "all others".
- **Code type:** which of an asset's codes a tag holds: QR, Micro QR or Code 128. Unset, it's the
  asset's first code as labels show it, QR first.

| Method | Path | Permission | Purpose |
| --- | --- | --- | --- |
| `GET` | `/api/v1/settings/rfid` | `settings.manage` | Read the settings: `scheme`, `company_prefix`, `filter` and `code_type`. |
| `PUT` | `/api/v1/settings/rfid` | `settings.manage` | Save them. |

## Writing tags while printing

Zebra's RFID printers, such as the ZD621R and ZT411 RFID, write a label's tag as they print it.
Set up the printer with the [`zpl`](printing.md#zpl) driver and turn on its `rfid` option. Each
label of an asset then has its tag written with the asset's EPC. A test print without an asset
writes none.

A label whose asset has no code of the type, or a code the scheme can't write, isn't printed:

- **Printing directly**, the job stops before anything is sent, saying which asset and why.
- **From the print queue**, that label stays in the queue, saying why, and the rest print.

When a tag won't take its EPC, the printer prints VOID over the label and tries the next, up to
`rfid_tries` labels, then does what `rfid_on_failure` says. BAM! sends the job and doesn't hear
back, so a failure shows at the printer, not in BAM!.

## Reading tags

A handheld reader in keyboard mode types the EPC it reads, in hexadecimal, as a barcode scanner
types a code. Type it, or let the reader type it, into the Scan page's box. BAM! looks it up as a
code first, then reads it under the instance's scheme and looks up the code it holds. A GIAI of
another company's prefix, or another filter value, isn't this instance's and finds nothing.

How a reader sends what it reads is set on the reader. Zebra's DS8 series, for one, sends the EPC
in hexadecimal by default, and can send the EPC URI or GS1 element string instead, or convert hex
to text. Only hexadecimal, or the code itself, is looked up.

A page for taking stock with a reader, many tags at once, is still to come.

## Sources

- Zebra, *ZPL II Programming Guide*: `^RS` (tag type, tries, error handling, void printing) and
  `^RF` (writing in hexadecimal, memory banks `E` and `A`).
  <https://docs.zebra.com/us/en/printers/software/zpl-pg/c-zpl-rfid-zpl-rfid-commands/r-zpl-rfid-rs.html>,
  <https://docs.zebra.com/us/en/printers/software/zpl-pg/c-zpl-rfid-zpl-rfid-commands/r-zpl-rfid-rf.html>
- Zebra, DS8 series *RFID Data Transmission Format*: how a reader sends an EPC, and published
  GIAI-96 and GIAI-202 examples, which BAM!'s tests check against.
  <https://docs.zebra.com/us/en/scanners/general/ds8-series/ds82-prg/c-rfid-scanning/r-rfid-preferences-parameter-defaults/r-param-desc-rfid-data-transmission-format.html>
- epcpy, GIAI scheme documentation: a GIAI-96 example with a 6-digit prefix.
  <https://epcpy-tools.readthedocs.io/en/stable/schemes/GIAI.html>
- GS1, *EPC Tag Data Standard*: the GIAI headers, partition table and character set.
  <https://ref.gs1.org/standards/tds/>
