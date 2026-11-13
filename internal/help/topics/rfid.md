# RFID tags

Long-range RFID tags (UHF, sometimes sold as RAIN RFID) are read from meters away, many at once, without lining anything up. A Zebra RFID printer, such as the ZD621R or ZT411 RFID, can write a label's tag with its asset's code as it prints the label.

## Writing tags while printing

1. In **Admin → Printers**, set up the printer with the ZPL driver, choosing the model the RFID one is a version of, and tick **rfid**.
2. Under **RFID tags** on the same page, choose how codes are written.
3. Print labels as usual. Each label of an asset has its tag written with that asset's code.

If an asset has no code a tag can hold, its label isn't printed: printing directly stops and names it, and the print queue keeps that label, saying why, and prints the rest. When a tag won't take its code, the printer prints VOID over the label and tries the next. **rfid_tries** sets how many labels it tries, and **rfid_on_failure** what it does after that. Look at the printer for these: BAM! doesn't hear about them.

## How codes are written

- **Text**, the default, writes the code's own characters. It needs nothing else, and a code of up to 12 characters fits the 96 bits most tags hold.
- **GIAI-96** and **GIAI-202** are GS1's standard for assets, which any reader knows. They need a GS1 company prefix, which GS1 sells. GIAI-96 writes only codes that are whole numbers; GIAI-202 writes letters and digits too, on tags of 208 bits or more.

**Code a tag holds** chooses which of an asset's codes is written. Left alone, it's the first code its labels show.

## Finding assets by their tags

Set a handheld reader to act as a keyboard, then read a tag with the cursor in the Scan page's box. The reader types what the tag holds, and the Scan page opens that asset. Readers usually type it in hexadecimal; BAM! turns that back into the code. A tag written by another organization finds nothing.

Nothing here has been tried on an RFID printer or reader yet. If you have one, tell us how it goes.
