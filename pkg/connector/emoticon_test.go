package connector

import (
	"encoding/hex"
	"testing"
)

// First 128 bytes of a real animated emoticon from the item CDN.
const encryptedEmoticonHead = "b5e4041d6a7d841931077e1431dd7ce044581ce9d1da1c50204def374b8b8185765cb5e43470a07aa3ae285b0a5f8e47cd4d82594c1dc3cd01d27b226b849eb6d62e60d4e6e5aed6f9b2585d6501276f87056c694b4f0a2a8309bc36240c27c5950a7c1c2c712b6f10263b960b25dba47962dee442dfd58d607a4fbc87ee8900"

func TestDecryptEmoticon(t *testing.T) {
	enc, err := hex.DecodeString(encryptedEmoticonHead)
	if err != nil {
		t.Fatal(err)
	}
	dec := decryptEmoticon(append(enc, make([]byte, 64)...))
	if string(dec[0:4]) != "RIFF" || string(dec[8:12]) != "WEBP" || string(dec[12:16]) != "VP8X" {
		t.Fatalf("decrypted header = %q", dec[:16])
	}
	if w, h := imageSize(dec); w <= 0 || h <= 0 || w > 1000 || h > 1000 {
		t.Errorf("imageSize = %dx%d", w, h)
	}
	t.Logf("header %q size %v", dec[:40], func() [2]int { w, h := imageSize(dec); return [2]int{w, h} }())
	if again := decryptEmoticon(dec); string(again[:4]) != "RIFF" {
		t.Error("an already decoded image was changed")
	}
}
