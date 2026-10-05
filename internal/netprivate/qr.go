package netprivate

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// QRSVG renders s as a QR code in SVG, for the app to show: a phone scans it to
// open Werkbord. It is vector, so it stays sharp at any size, and carries
// nothing but the encoded text.
func QRSVG(s string) (string, error) {
	q, err := qrcode.New(s, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bm := q.Bitmap() // includes the quiet zone
	n := len(bm)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="QR code"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n, n, n)
	for y, row := range bm {
		for x := 0; x < n; {
			if !row[x] {
				x++
				continue
			}
			start := x
			for x < n && row[x] {
				x++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", start, y, x-start, x-start)
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}

// QRText renders s as a QR code for a terminal, two rows of modules per line of
// text, so it is small enough to fit on a screen. Dark modules are drawn light,
// as on a dark terminal; modern phone cameras read either polarity.
func QRText(s string) (string, error) {
	q, err := qrcode.New(s, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bm := q.Bitmap()
	var b strings.Builder
	for y := 0; y < len(bm); y += 2 {
		for x := range bm[y] {
			top := bm[y][x]
			bottom := y+1 < len(bm) && bm[y+1][x]
			switch {
			case top && bottom:
				b.WriteString(" ")
			case top:
				b.WriteString("▄")
			case bottom:
				b.WriteString("▀")
			default:
				b.WriteString("█")
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
