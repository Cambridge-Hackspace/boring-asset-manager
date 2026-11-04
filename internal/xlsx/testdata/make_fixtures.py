"""Writes the workbooks the reader is tested against: the same sheet, Tools,
as Excel, LibreOffice and Google Sheets each write one, by the parts and
cell types each is known to use. They are written here, not saved from
those programs, so that they are the same on every run; a workbook saved
from one of them can be dropped in beside them.

    python3 internal/xlsx/testdata/make_fixtures.py

Each holds, past an empty first row and column:

    Name    Quantity  Bought      Working  Total
    Saw     0.1       2024-03-05  true     0.2     (=C3*2, cached)
    Clamp   5         2023-12-31  false    10      (=C4*2, cached)
and a second sheet, Bins, with one column, Name, and one row, Parts bin A3 by the lathe.
"""
import os
import zipfile
from html import escape

HERE = os.path.dirname(os.path.abspath(__file__))
NS = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main'
REL = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships'

CONTENT_TYPES = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>'''
ROOT_RELS = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>'''


def workbook(sheets):
    items = ''.join(f'<sheet name="{n}" sheetId="{i + 1}" r:id="rId{i + 1}"/>' for i, n in enumerate(sheets))
    return f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<workbook xmlns="{NS}" xmlns:r="{REL}"><sheets>{items}</sheets></workbook>'


def rels(targets):
    items = ''.join(
        f'<Relationship Id="rId{i + 1}" Type="{REL}/worksheet" Target="{t}"/>' for i, t in enumerate(targets))
    return f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">{items}</Relationships>'


def sheet(rows, merges=''):
    return f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<worksheet xmlns="{NS}"><sheetData>{"".join(rows)}</sheetData>{merges}</worksheet>'


def shared(strings):
    items = ''.join(f'<si><t>{s}</t></si>' for s in strings)
    return f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<sst xmlns="{NS}" count="{len(strings)}" uniqueCount="{len(strings)}">{items}</sst>'


def styles(numfmts, xfs):
    fmts = ''.join(f'<numFmt numFmtId="{i}" formatCode="{escape(c, quote=True)}"/>' for i, c in numfmts)
    cells = ''.join(f'<xf numFmtId="{n}" fontId="0" fillId="0" borderId="0"/>' for n in xfs)
    head = f'<numFmts count="{len(numfmts)}">{fmts}</numFmts>' if numfmts else ''
    return f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<styleSheet xmlns="{NS}">{head}<cellXfs count="{len(xfs)}">{cells}</cellXfs></styleSheet>'


def write(name, parts):
    path = os.path.join(HERE, name)
    with zipfile.ZipFile(path, 'w', zipfile.ZIP_DEFLATED) as z:
        for part, data in parts.items():
            info = zipfile.ZipInfo(part, date_time=(2026, 11, 4, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, data)
    print('wrote', path)


STRINGS = ['Name', 'Quantity', 'Bought', 'Working', 'Total', 'Saw', 'Clamp', 'Parts bin A3 by the lathe']
S = {s: i for i, s in enumerate(STRINGS)}
# 2024-03-05 and 2023-12-31 as 1900 serials.
MARCH, DECEMBER = 45356, 45291


def excel():
    # Shared strings for text, a built-in date format (14), booleans as
    # t="b", and formulas with their cached values. Cells are named.
    def s(ref, text):
        return f'<c r="{ref}" t="s"><v>{S[text]}</v></c>'
    tools = [
        '<row r="2">' + ''.join(s(f'{c}2', t) for c, t in zip('BCDEF', STRINGS[:5])) + '</row>',
        f'<row r="3">{s("B3", "Saw")}<c r="C3"><v>0.10000000000000001</v></c><c r="D3" s="1"><v>{MARCH}</v></c>'
        '<c r="E3" t="b"><v>1</v></c><c r="F3"><f>C3*2</f><v>0.2</v></c></row>',
        f'<row r="4">{s("B4", "Clamp")}<c r="C4"><v>5</v></c><c r="D4" s="1"><v>{DECEMBER}</v></c>'
        '<c r="E4" t="b"><v>0</v></c><c r="F4"><f>C4*2</f><v>10</v></c></row>',
    ]
    bins = [f'<row r="1">{s("A1", "Name")}</row>', f'<row r="2">{s("A2", "Parts bin A3 by the lathe")}</row>']
    write('excel.xlsx', {
        '[Content_Types].xml': CONTENT_TYPES, '_rels/.rels': ROOT_RELS,
        'xl/workbook.xml': workbook(['Tools', 'Bins']),
        'xl/_rels/workbook.xml.rels': rels(['worksheets/sheet1.xml', 'worksheets/sheet2.xml']),
        'xl/sharedStrings.xml': shared(STRINGS),
        'xl/styles.xml': styles([], [0, 14]),
        'xl/worksheets/sheet1.xml': sheet(tools), 'xl/worksheets/sheet2.xml': sheet(bins),
    })


def libreoffice():
    # A custom date format (164), formulas whose text results are t="str",
    # and rows written without cell names past the first.
    def s(ref, text):
        return f'<c r="{ref}" t="s"><v>{S[text]}</v></c>'
    tools = [
        '<row r="2">' + ''.join(s(f'{c}2', t) for c, t in zip('BCDEF', STRINGS[:5])) + '</row>',
        f'<row r="3">{s("B3", "Saw")}<c><v>0.1</v></c><c s="1"><v>{MARCH}</v></c>'
        '<c t="b"><v>1</v></c><c t="str"><f>TEXT(C3*2;"0.0#")</f><v>0.2</v></c></row>',
        f'<row r="4">{s("B4", "Clamp")}<c><v>5</v></c><c s="1"><v>{DECEMBER}</v></c>'
        '<c t="b"><v>0</v></c><c><f>C4*2</f><v>10</v></c></row>',
    ]
    bins = [f'<row r="1">{s("A1", "Name")}</row>', f'<row r="2">{s("A2", "Parts bin A3 by the lathe")}</row>']
    write('libreoffice.xlsx', {
        '[Content_Types].xml': CONTENT_TYPES, '_rels/.rels': ROOT_RELS,
        'xl/workbook.xml': workbook(['Tools', 'Bins']),
        'xl/_rels/workbook.xml.rels': rels(['worksheets/sheet1.xml', 'worksheets/sheet2.xml']),
        'xl/sharedStrings.xml': shared(STRINGS),
        'xl/styles.xml': styles([(164, 'YYYY\\-MM\\-DD')], [0, 164]),
        'xl/worksheets/sheet1.xml': sheet(tools), 'xl/worksheets/sheet2.xml': sheet(bins),
    })


def google():
    # Text written inline, parts named from the package's root, and a date
    # format with a quoted literal.
    def s(ref, text):
        return f'<c r="{ref}" t="inlineStr"><is><t>{text}</t></is></c>'
    tools = [
        '<row r="2">' + ''.join(s(f'{c}2', t) for c, t in zip('BCDEF', STRINGS[:5])) + '</row>',
        f'<row r="3">{s("B3", "Saw")}<c r="C3"><v>0.1</v></c><c r="D3" s="1"><v>{MARCH}</v></c>'
        '<c r="E3" t="b"><v>1</v></c><c r="F3"><f>C3*2</f><v>0.2</v></c></row>',
        f'<row r="4">{s("B4", "Clamp")}<c r="C4"><v>5</v></c><c r="D4" s="1"><v>{DECEMBER}</v></c>'
        '<c r="E4" t="b"><v>0</v></c><c r="F4"><f>C4*2</f><v>10</v></c></row>',
    ]
    bins = [f'<row r="1">{s("A1", "Name")}</row>', f'<row r="2">{s("A2", "Parts bin A3 by the lathe")}</row>']
    write('google.xlsx', {
        '[Content_Types].xml': CONTENT_TYPES, '_rels/.rels': ROOT_RELS,
        'xl/workbook.xml': workbook(['Tools', 'Bins']),
        'xl/_rels/workbook.xml.rels': rels(['/xl/worksheets/sheet1.xml', '/xl/worksheets/sheet2.xml']),
        'xl/styles.xml': styles([(165, 'yyyy"-"mm"-"dd')], [0, 165]),
        'xl/worksheets/sheet1.xml': sheet(tools), 'xl/worksheets/sheet2.xml': sheet(bins),
    })


excel()
libreoffice()
google()
