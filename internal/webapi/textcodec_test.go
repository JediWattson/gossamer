package webapi_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/webapi"
)

func TestUTF8TextCodec(t *testing.T) {
	encoded := webapi.EncodeUTF8("A🙂")
	if !reflect.DeepEqual(encoded, []byte{0x41, 0xf0, 0x9f, 0x99, 0x82}) {
		t.Fatalf("encoded = %#v", encoded)
	}
	decoded, err := webapi.DecodeUTF8(encoded, true)
	if err != nil || decoded != "A🙂" {
		t.Fatalf("decoded = %q, %v", decoded, err)
	}
	if _, err := webapi.DecodeUTF8([]byte{0xff}, true); err == nil {
		t.Fatal("fatal decoder accepted invalid UTF-8")
	}
	if decoded, err := webapi.DecodeUTF8([]byte{0xff}, false); err != nil || decoded != "�" {
		t.Fatalf("replacement decoded = %q, %v", decoded, err)
	}
	if label, ok := webapi.NormalizeUTF8Label(" UTF8 "); !ok || label != "utf-8" {
		t.Fatalf("normalized label = %q, %t", label, ok)
	}
}
