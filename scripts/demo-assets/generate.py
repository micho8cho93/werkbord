#!/usr/bin/env python3
"""Writes the three small sample files for the Demo · Launch room Files view: a one-page PDF, a picture and a Markdown note.

They are invented and carry no real brand, person or product. Run it again to rewrite them; nothing here is uploaded:
open the Files view, choose an entry and use Attach file (or Add files) to read each one in the app.
"""
import struct
import zlib
from pathlib import Path

here = Path(__file__).parent


def pdf(lines):
    """A one-page PDF in Helvetica. Lines of (size, text); text may not contain parentheses or backslashes."""
    content = ['BT']
    y = 760
    for size, text in lines:
        content.append(f'/F1 {size} Tf 1 0 0 1 56 {y} Tm ({text}) Tj')
        y -= int(size * 1.7)
    content.append('ET')
    stream = '\n'.join(content).encode('latin-1')
    objects = [
        b'<< /Type /Catalog /Pages 2 0 R >>',
        b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
        b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R /F2 6 0 R >> >> /Contents 5 0 R >>',
        b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
        b'<< /Length %d >>\nstream\n' % len(stream) + stream + b'\nendstream',
        b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>',
    ]
    out = bytearray(b'%PDF-1.4\n')
    offsets = []
    for number, body in enumerate(objects, 1):
        offsets.append(len(out))
        out += b'%d 0 obj\n' % number + body + b'\nendobj\n'
    xref = len(out)
    out += b'xref\n0 %d\n0000000000 65535 f \n' % (len(objects) + 1)
    for offset in offsets:
        out += b'%010d 00000 n \n' % offset
    out += b'trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n' % (len(objects) + 1, xref)
    return bytes(out)


def png(width, height, pixel):
    rows = b''.join(b'\x00' + b''.join(bytes(pixel(x, y)) for x in range(width)) for y in range(height))

    def chunk(kind, data):
        body = kind + data
        return struct.pack('>I', len(data)) + body + struct.pack('>I', zlib.crc32(body) & 0xFFFFFFFF)
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(rows, 9)) + chunk(b'IEND', b'')


def palette(x, y):
    swatches = [(36, 71, 255), (242, 169, 59), (47, 143, 85), (194, 65, 59), (123, 79, 192), (10, 10, 10)]
    if y < 8 or y >= 152:
        return (245, 245, 242)
    index = x * len(swatches) // 360
    edge = x % 60 in (0, 59)
    return (245, 245, 242) if edge else swatches[index]


(here / 'lantern-launch-one-pager.pdf').write_bytes(pdf([
    (22, 'Lantern: launch one-pager'),
    (11, '[Demo] A fictional product, invented for sample data.'),
    (13, ''),
    (14, 'What it is'),
    (11, 'Lantern keeps a small team\'s next step in view: one calm board, one place for notes.'),
    (13, ''),
    (14, 'Who it is for'),
    (11, 'Independent builders and small studios who want fewer tools and clearer handoffs.'),
    (13, ''),
    (14, 'Launch week'),
    (11, 'Monday: announce.  Wednesday: walkthrough session.  Friday: first review of feedback.'),
    (13, ''),
    (10, 'Contact: hello@demo.example.test'),
]))
(here / 'lantern-colours.png').write_bytes(png(360, 160, palette))
(here / 'lantern-announcement.md').write_text("""# Lantern is here

*[Demo] A fictional announcement, written for sample data.*

Lantern keeps the next step of your work in view. It is **one calm board**, **one place for notes**, and
nothing else asking for your attention.

## What you get

- A board that shows what is next, not everything that exists
- Notes that sit beside the work they explain
- A weekly review that takes ten minutes

## Try it this week

1. Add the three things that matter most
2. Write one note about why
3. Come back on Friday and look at what moved

> Fewer tools. Clearer handoffs.

Questions? Write to hello@demo.example.test.
""")
print('Wrote', *sorted(p.name for p in here.iterdir() if p.name != 'generate.py'))
