package render

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"io"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"

	"golang.org/x/image/draw"
)

// WritePDF writes one page per scene, each sized exactly to its label. Text
// and barcodes are vector paths; images are embedded at their own resolution.
// The output contains no timestamps or random identifiers, so the same scenes
// always produce the same bytes.
func WritePDF(w io.Writer, scenes ...*Scene) error {
	if len(scenes) == 0 {
		return fmt.Errorf("pdf: no pages")
	}
	pw := &pdfWriter{}
	pw.buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	// Objects 1 and 2 are the catalog and page tree; pages follow.
	pw.offsets = make([]int, 2)
	var kids []string
	for _, s := range scenes {
		pageID, err := pw.writePage(s)
		if err != nil {
			return err
		}
		kids = append(kids, fmt.Sprintf("%d 0 R", pageID))
	}
	pw.writeObjectAt(1, "<< /Type /Catalog /Pages 2 0 R >>")
	pw.writeObjectAt(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids)))

	xref := pw.buf.Len()
	fmt.Fprintf(&pw.buf, "xref\n0 %d\n0000000000 65535 f \n", len(pw.offsets)+1)
	for _, off := range pw.offsets {
		fmt.Fprintf(&pw.buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&pw.buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(pw.offsets)+1, xref)

	_, err := w.Write(pw.buf.Bytes())
	return err
}

type pdfWriter struct {
	buf bytes.Buffer
	// offsets[i] is the byte offset of object i+1.
	offsets []int
}

func (pw *pdfWriter) reserve() int {
	pw.offsets = append(pw.offsets, 0)
	return len(pw.offsets)
}

func (pw *pdfWriter) writeObjectAt(id int, body string) {
	pw.offsets[id-1] = pw.buf.Len()
	fmt.Fprintf(&pw.buf, "%d 0 obj\n%s\nendobj\n", id, body)
}

func (pw *pdfWriter) writeStream(dict string, data []byte) int {
	id := pw.reserve()
	pw.offsets[id-1] = pw.buf.Len()
	fmt.Fprintf(&pw.buf, "%d 0 obj\n<< %s /Filter /FlateDecode /Length %d >>\nstream\n", id, dict, len(data))
	pw.buf.Write(data)
	pw.buf.WriteString("\nendstream\nendobj\n")
	return id
}

func (pw *pdfWriter) writePage(s *Scene) (int, error) {
	scale := 72 / float64(s.DPI) // points per dot
	var content bytes.Buffer
	// Draw in dots with the origin at the top-left, like every other backend.
	fmt.Fprintf(&content, "%s 0 0 %s 0 %s cm\n0 g\n", num(scale), num(-scale), num(float64(s.Height)*scale))

	var xobjects []string
	for i := 0; i < len(s.Items); i++ {
		it := &s.Items[i]
		switch {
		case it.Image != nil:
			pi := it.Image
			// Over nothing but the white label, an image is composited over
			// white; over what is drawn before it, its transparency is kept.
			id, err := pw.writeImage(pi.Image, i > 0)
			if err != nil {
				return 0, err
			}
			name := fmt.Sprintf("Im%d", len(xobjects))
			xobjects = append(xobjects, fmt.Sprintf("/%s %d 0 R", name, id))
			content.WriteString("q ")
			if pi.Rotation != 0 {
				// Turned clockwise, as dots run down the page, about its center.
				sin, cos := labels.SinCos(pi.Rotation)
				fmt.Fprintf(&content, "%s %s %s %s %s %s cm ", fine(cos), fine(sin), fine(-sin), fine(cos),
					fine(pi.Cx-pi.Cx*cos+pi.Cy*sin), fine(pi.Cy-pi.Cx*sin-pi.Cy*cos))
			}
			// The image's unit square has its first row at the top (v = 1).
			fmt.Fprintf(&content, "%d 0 0 %d %d %d cm /%s Do Q\n", pi.W, -pi.H, pi.X, pi.Y+pi.H, name)
		case it.Rects != nil:
			// The modules of codes drawn one after another are filled at once.
			for ; i < len(s.Items) && s.Items[i].Rects != nil; i++ {
				for _, r := range s.Items[i].Rects {
					fmt.Fprintf(&content, "%d %d %d %d re\n", r.X, r.Y, r.W, r.H)
				}
			}
			i--
			content.WriteString("f\n")
		default:
			pdfPath(&content, it.Path)
			content.WriteString("f\n")
		}
	}

	contentID := pw.writeStream("", deflate(content.Bytes()))
	pageID := pw.reserve()
	resources := "<< >>"
	if len(xobjects) > 0 {
		resources = fmt.Sprintf("<< /XObject << %s >> >>", strings.Join(xobjects, " "))
	}
	pw.writeObjectAt(pageID, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources %s /Contents %d 0 R >>",
		num(float64(s.Width)*scale), num(float64(s.Height)*scale), resources, contentID))
	return pageID, nil
}

// fine formats a number to six decimals, for a turn's matrix, whose
// rounding is multiplied across the image.
func fine(v float64) string {
	s := strings.TrimRight(strconv.FormatFloat(v, 'f', 6, 64), "0")
	s = strings.TrimSuffix(s, ".")
	if s == "-0" {
		return "0"
	}
	return s
}

// pdfPath writes a path's construction operators, without painting it.
func pdfPath(content *bytes.Buffer, p Path) {
	var cur Point
	for _, seg := range p {
		a, b, c := seg.Pts[0], seg.Pts[1], seg.Pts[2]
		switch seg.Op {
		case MoveTo:
			fmt.Fprintf(content, "%s %s m\n", num(a.X), num(a.Y))
			cur = a
		case LineTo:
			fmt.Fprintf(content, "%s %s l\n", num(a.X), num(a.Y))
			cur = a
		case QuadTo:
			// PDF has only cubic curves; raise the quadratic's degree.
			c1 := Point{cur.X + 2.0/3*(a.X-cur.X), cur.Y + 2.0/3*(a.Y-cur.Y)}
			c2 := Point{b.X + 2.0/3*(a.X-b.X), b.Y + 2.0/3*(a.Y-b.Y)}
			fmt.Fprintf(content, "%s %s %s %s %s %s c\n", num(c1.X), num(c1.Y), num(c2.X), num(c2.Y), num(b.X), num(b.Y))
			cur = b
		case CubeTo:
			fmt.Fprintf(content, "%s %s %s %s %s %s c\n", num(a.X), num(a.Y), num(b.X), num(b.Y), num(c.X), num(c.Y))
			cur = c
		}
	}
}

// writeImage embeds img as 8-bit RGB. Composited over white, it is opaque;
// otherwise, when it has any transparency, its alpha is a soft mask, so
// what is drawn before it shows through.
func (pw *pdfWriter) writeImage(img image.Image, keepAlpha bool) (int, error) {
	b := img.Bounds()
	if keepAlpha && !opaque(img) {
		nrgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(nrgba, nrgba.Bounds(), img, b.Min, draw.Src)
		raw := make([]byte, 0, b.Dx()*b.Dy()*3)
		alpha := make([]byte, 0, b.Dx()*b.Dy())
		for i := 0; i < len(nrgba.Pix); i += 4 {
			raw = append(raw, nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2])
			alpha = append(alpha, nrgba.Pix[i+3])
		}
		mask := pw.writeStream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8", b.Dx(), b.Dy()), deflate(alpha))
		dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask %d 0 R", b.Dx(), b.Dy(), mask)
		return pw.writeStream(dict, deflate(raw)), nil
	}

	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Over)

	raw := make([]byte, 0, b.Dx()*b.Dy()*3)
	for i := 0; i < len(rgba.Pix); i += 4 {
		raw = append(raw, rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2])
	}
	dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8", b.Dx(), b.Dy())
	return pw.writeStream(dict, deflate(raw)), nil
}

// opaque reports whether every pixel of img is fully opaque.
func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xFFFF {
				return false
			}
		}
	}
	return true
}

func deflate(data []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}
