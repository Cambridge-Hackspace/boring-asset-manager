package render

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"io"
	"strconv"
)

// WriteSVG writes the scene as an SVG document sized to the label's physical
// dimensions, with a viewBox in dots.
func WriteSVG(w io.Writer, s *Scene) error {
	bw := bufio.NewWriter(w)
	pw, ph := s.PhysicalSize()
	fmt.Fprintf(bw, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprintf(bw, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%s%s" height="%s%s" viewBox="0 0 %d %d">`+"\n",
		num(pw), s.Units, num(ph), s.Units, s.Width, s.Height)
	fmt.Fprintf(bw, `<rect width="%d" height="%d" fill="#fff"/>`+"\n", s.Width, s.Height)

	for i := 0; i < len(s.Items); i++ {
		it := &s.Items[i]
		switch {
		case it.Image != nil:
			if err := svgImage(bw, it.Image); err != nil {
				return err
			}
		case it.Rects != nil:
			// Rectangles drawn one after another in one paint are one path.
			fmt.Fprintf(bw, `<path fill="%s" shape-rendering="crispEdges" d="`, svgColor(it.Paint))
			for ; i < len(s.Items) && s.Items[i].Rects != nil && s.Items[i].Paint == it.Paint; i++ {
				for _, r := range s.Items[i].Rects {
					fmt.Fprintf(bw, "M%d %dh%dv%dh-%dz", r.X, r.Y, r.W, r.H, r.W)
				}
			}
			i--
			bw.WriteString(`"/>` + "\n")
		default:
			svgPath(bw, it.Path, it.Paint)
		}
	}

	bw.WriteString("</svg>\n")
	return bw.Flush()
}

// svgImage writes an image, embedded as PNG.
func svgImage(bw *bufio.Writer, pi *PlacedImage) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, pi.Image); err != nil {
		return fmt.Errorf("encoding image: %w", err)
	}
	turn := ""
	if pi.Rotation != 0 {
		turn = fmt.Sprintf(` transform="rotate(%s %s %s)"`, num(pi.Rotation), num(pi.Cx), num(pi.Cy))
	}
	fmt.Fprintf(bw, `<image x="%d" y="%d" width="%d" height="%d"%s preserveAspectRatio="none" xlink:href="data:image/png;base64,%s"/>`+"\n",
		pi.X, pi.Y, pi.W, pi.H, turn, base64.StdEncoding.EncodeToString(buf.Bytes()))
	return nil
}

// svgColor is a paint's color.
func svgColor(p Paint) string {
	if p == White {
		return "#fff"
	}
	return "#000"
}

// svgPath writes a filled path.
func svgPath(bw *bufio.Writer, p Path, paint Paint) {
	fmt.Fprintf(bw, `<path fill="%s" d="`, svgColor(paint))
	for _, seg := range p {
		a, b, c := seg.Pts[0], seg.Pts[1], seg.Pts[2]
		switch seg.Op {
		case MoveTo:
			fmt.Fprintf(bw, "M%s %s", num(a.X), num(a.Y))
		case LineTo:
			fmt.Fprintf(bw, "L%s %s", num(a.X), num(a.Y))
		case QuadTo:
			fmt.Fprintf(bw, "Q%s %s %s %s", num(a.X), num(a.Y), num(b.X), num(b.Y))
		case CubeTo:
			fmt.Fprintf(bw, "C%s %s %s %s %s %s", num(a.X), num(a.Y), num(b.X), num(b.Y), num(c.X), num(c.Y))
		}
	}
	bw.WriteString(`Z"/>` + "\n")
}

// num formats a coordinate with at most three decimals, so output is compact
// and identical across platforms.
func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	for s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	if s == "-0" {
		return "0"
	}
	return s
}
