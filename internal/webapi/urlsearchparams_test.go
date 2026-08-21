package webapi_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/webapi"
)

func TestURLSearchParamsPreservesOrderDuplicatesAndFormEncoding(t *testing.T) {
	params := webapi.ParseURLSearchParams("?tag=one+two&tag=three&empty=&encoded=%7E")
	if value, ok := params.Get("tag"); !ok || value != "one two" {
		t.Fatalf("Get(tag) = %q, %t", value, ok)
	}
	if got := params.GetAll("tag"); !reflect.DeepEqual(got, []string{"one two", "three"}) {
		t.Fatalf("GetAll(tag) = %#v", got)
	}
	params.Set("tag", "replacement")
	params.Append("tag", "last")
	remove := "last"
	params.Delete("tag", &remove)
	if got := params.String(); got != "tag=replacement&empty=&encoded=%7E" {
		t.Fatalf("serialized params = %q", got)
	}
}

func TestURLSearchParamsStableSort(t *testing.T) {
	params := webapi.NewURLSearchParams([]webapi.SearchParam{
		{Name: "z", Value: "first"}, {Name: "a", Value: "one"},
		{Name: "z", Value: "second"}, {Name: "a", Value: "two"},
	})
	params.Sort()
	if got := params.String(); got != "a=one&a=two&z=first&z=second" {
		t.Fatalf("sorted params = %q", got)
	}
}
