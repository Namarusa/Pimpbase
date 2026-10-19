package xmltree

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <Catalog count="2">
      <Book id="1"><Title>Dune</Title><Price>9.50</Price><Tag>sf</Tag><Tag>classic</Tag></Book>
      <Book id="2"><Title><![CDATA[Fish & Chips]]></Title><Price currency="EUR">12</Price><Empty/></Book>
      <Note>two &amp; more</Note>
    </Catalog>
  </soap:Body>
</soap:Envelope>`
	got, err := Decode(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(got)
	js := strings.TrimSpace(buf.String())
	want := `{"Envelope":{"Body":{"Catalog":{"@count":"2","Book":[{"@id":"1","Price":"9.50","Tag":["sf","classic"],"Title":"Dune"},{"@id":"2","Empty":"","Price":{"#text":"12","@currency":"EUR"},"Title":"Fish & Chips"}],"Note":"two & more"}}}}`
	if js != want {
		t.Errorf("got  %s\nwant %s", js, want)
	}

	for _, bad := range []string{"", "   ", "<a><b></a>", "<a>"} {
		if _, err := Decode(strings.NewReader(bad)); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

func TestEncode(t *testing.T) {
	v := map[string]any{
		"order": map[string]any{
			"@id":   "7",
			"item":  []any{map[string]any{"sku": "A<1>", "qty": 2.0}, map[string]any{"sku": "B", "qty": 1.5}},
			"note":  nil,
			"rush":  true,
			"1 bad": "x",
		},
	}
	got := Encode("", v)
	want := `<order id="7"><__bad>x</__bad><item><qty>2</qty><sku>A&lt;1&gt;</sku></item><item><qty>1.5</qty><sku>B</sku></item><note/><rush>true</rush></order>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := Encode("id", []any{"1", "2"}); got != "<id>1</id><id>2</id>" {
		t.Errorf("list = %s", got)
	}
	// What Decode reads, Encode writes back.
	back, err := Decode(strings.NewReader(Encode("", v)))
	if err != nil {
		t.Fatal(err)
	}
	if back["order"].(map[string]any)["item"].([]any)[0].(map[string]any)["sku"] != "A<1>" {
		t.Errorf("round trip = %v", back)
	}
}

func TestCheck(t *testing.T) {
	if err := Check(`<a x="1"><b/></a>`); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "text", "<a><b></a>", "<a/><b/>", `<a x=1/>`} {
		if Check(bad) == nil {
			t.Errorf("Check(%q) passed", bad)
		}
	}
}
