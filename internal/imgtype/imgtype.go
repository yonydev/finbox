package imgtype

import "bytes"

type Type int

const (
	JPEG Type = iota + 1
	PNG
	WebP
	PDF
)

func (t Type) Ext() string {
	switch t {
	case JPEG:
		return ".jpg"
	case PNG:
		return ".png"
	case WebP:
		return ".webp"
	case PDF:
		return ".pdf"
	}
	return ""
}

func (t Type) MIME() string {
	switch t {
	case JPEG:
		return "image/jpeg"
	case PNG:
		return "image/png"
	case WebP:
		return "image/webp"
	case PDF:
		return "application/pdf"
	}
	return ""
}

// Sniff identifies JPEG/PNG/WebP/PDF from the first bytes. Needs ≥12 bytes.
func Sniff(head []byte) (Type, bool) {
	if len(head) < 12 {
		return 0, false
	}
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return JPEG, true
	case bytes.HasPrefix(head, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return PNG, true
	case bytes.HasPrefix(head, []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return WebP, true
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return PDF, true
	}
	return 0, false
}
