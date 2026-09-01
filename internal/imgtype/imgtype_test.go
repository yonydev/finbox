package imgtype

import "testing"

func TestSniff(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), 0)
	txt := []byte("hello world, not an image")

	if ty, ok := Sniff(jpeg); !ok || ty != JPEG || ty.Ext() != ".jpg" || ty.MIME() != "image/jpeg" {
		t.Errorf("jpeg: %v %v", ty, ok)
	}
	if ty, ok := Sniff(png); !ok || ty != PNG {
		t.Errorf("png: %v %v", ty, ok)
	}
	if ty, ok := Sniff(webp); !ok || ty != WebP {
		t.Errorf("webp: %v %v", ty, ok)
	}
	if _, ok := Sniff(txt); ok {
		t.Error("txt sniffed as image")
	}
	if _, ok := Sniff([]byte{0xFF}); ok {
		t.Error("short input sniffed as image")
	}
}
