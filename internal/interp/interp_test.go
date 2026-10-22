package interp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func scope() Map {
	calls := 0
	return Map{
		"params": map[string]any{"id": "a b/c", "limit": 10.0, "name": `Ada "the" <first>`, "tags": []any{"x", "y"}, "on": true, "blank": ""},
		"env":    map[string]any{"API_URL": "https://api.example.com/v1", "TOKEN": "s3cret"},
		"user":   map[string]any{"email": "Ada@Example.com"},
		"queries": map[string]any{"Login": Lazy(func(rest []string) (any, error) {
			calls++
			return Walk(map[string]any{"body": map[string]any{"token": "tok-1", "n": calls}}, rest)
		})},
		"fail": Lazy(func([]string) (any, error) { return nil, errors.New("login failed") }),
	}
}

func render(t *testing.T, mode Mode, src string) string {
	t.Helper()
	tpl, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	out, err := tpl.Render(mode, scope())
	if err != nil {
		t.Fatalf("Render(%q): %v", src, err)
	}
	return out
}

func TestRender(t *testing.T) {
	cases := []struct {
		mode      Mode
		src, want string
	}{
		{Text, "Hello {{ user.email | lower }}!", "Hello ada@example.com!"},
		{Text, "{{params.limit}} rows, {{ params.missing | default:\"none\" }}", "10 rows, none"},
		{Text, "{{ params.tags | join:\" + \" }} / {{ params.tags | count }} / {{ params.tags | last }}", "x + y / 2 / y"},
		{Text, "{{ queries.Login.body.token }}", "tok-1"},
		{Text, `{{ = CONCAT("Bearer ", env.TOKEN) }}`, "Bearer s3cret"},
		{Text, `{{ = IF([limit] > 5, "many", "few") }}`, "many"},
		{Text, `{{ = UPPER(LEFT(user.email, 3)) }}`, "ADA"},
		{Text, `{{ params.limit | number:2 }}`, "10.00"},
		{Text, `{{ env.TOKEN | base64 }}`, "czNjcmV0"},
		{Text, `{{ PARAMS.ID }}`, "a b/c"},

		{URL, "{{ env.API_URL }}/items/{{ params.id }}?q={{ params.name }}&n={{ params.limit }}", "https://api.example.com/v1/items/a%20b%2Fc?q=Ada+%22the%22+%3Cfirst%3E&n=10"},
		{URL, "/files/{{ params.id | raw }}", "/files/a b/c"},

		{Header, "Bearer {{ params.evil }}", "Bearer "},
		{JSON, `{"id": "{{ params.id }}", "name": "{{ params.name }}", "limit": {{ params.limit }}, "tags": {{ params.tags }}, "on": {{ params.on }}, "none": {{ params.missing }}}`,
			`{"id": "a b/c", "name": "Ada \"the\" <first>", "limit": 10, "tags": ["x","y"], "on": true, "none": null}`},
		{JSON, `{"note": "say \"{{ params.id }}\"", "raw": "{{ params.tags | json }}", "obj": {{ params.tags | json }}}`, `{"note": "say \"a b/c\"", "raw": "[\"x\",\"y\"]", "obj": ["x","y"]}`},
		{JSON, `{"text": {{ params.id }}}`, `{"text": "a b/c"}`},

		{XML, `<user name="{{ params.name }}"><id>{{ params.id }}</id>{{ params.tags | xml:"tag" }}</user>`,
			`<user name="Ada &quot;the&quot; &lt;first&gt;"><id>a b/c</id><tag>x</tag><tag>y</tag></user>`},
		{XML, `<n>{{ params.name | xml }}</n><j>{{ params.tags | json }}</j>`, `<n>Ada &quot;the&quot; &lt;first&gt;</n><j>[&quot;x&quot;,&quot;y&quot;]</j>`},
	}
	for _, c := range cases {
		if got := render(t, c.mode, c.src); got != c.want {
			t.Errorf("Render(%q)\n got %s\nwant %s", c.src, got, c.want)
		}
	}
	// Headers never carry line breaks, whatever the value.
	tpl := MustParse("x-{{ v }}")
	if got, _ := tpl.Render(Header, Map{"v": "a\r\nSet-Cookie: b"}); strings.ContainsAny(got, "\r\n") {
		t.Errorf("header = %q", got)
	}
	if got, _ := MustParse("{{ v | raw }}").Render(Header, Map{"v": "a\nb"}); got != "a b" {
		t.Errorf("raw header = %q", got)
	}
	// The JSON a body produces is valid.
	out := render(t, JSON, `{"a": "{{ params.name }}", "b": [{{ params.limit }}, "{{ params.tags }}"]}`)
	if !json.Valid([]byte(out)) {
		t.Errorf("invalid JSON %s", out)
	}
}

func TestValue(t *testing.T) {
	v, err := MustParse("{{ params.tags }}").Value(scope())
	if err != nil || len(v.([]any)) != 2 {
		t.Errorf("Value = %v, %v", v, err)
	}
	v, _ = MustParse("n={{ params.limit }}").Value(scope())
	if v != "n=10" {
		t.Errorf("Value = %v", v)
	}
}

func TestErrors(t *testing.T) {
	for _, src := range []string{"{{ params.id", "{{ }}", "{{ params.id | shout }}", "{{ = CONCAT( }}", "{{ = NOPE(1) }}"} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) succeeded", src)
		}
	}
	if _, err := MustParse("{{ fail.x }}").Render(Text, scope()); err == nil || err.Error() != "login failed" {
		t.Errorf("lazy error = %v", err)
	}
	if _, err := MustParse(`{{ = CONCAT("x", fail.y) }}`).Render(Text, scope()); err == nil {
		t.Error("formula lookup error was lost")
	}
}

func TestPaths(t *testing.T) {
	got := MustParse(`{{ queries.Login.body.token }} {{ = CONCAT([id], env.TOKEN) }} plain`).Paths()
	want := "queries.Login.body.token|params.id|env.TOKEN"
	var s []string
	for _, p := range got {
		s = append(s, strings.Join(p, "."))
	}
	if strings.Join(s, "|") != want {
		t.Errorf("Paths = %v", s)
	}
}

func TestBracketPaths(t *testing.T) {
	s := Map{"steps": map[string]any{"Api": map[string]any{"rows": []any{
		map[string]any{"name": "Ada", "first name": "A", "a.b": 1.0, "tags": []any{"x", "y"}},
	}}}}
	for src, want := range map[string]string{
		`{{ steps.Api.rows.0.name }}`:               "Ada",
		`{{ steps.Api.rows[0].name }}`:              "Ada",
		`{{ steps.Api.rows[0]["name"] }}`:           "Ada",
		`{{ steps.Api.rows[ 0 ][ 'first name' ] }}`: "A",
		`{{ steps.Api.rows.0["a.b"] }}`:             "1",
		`{{ steps.Api.rows.0.tags[1] | upper }}`:    "Y",
	} {
		out, err := MustParse(src).Render(Text, s)
		if err != nil || out != want {
			t.Errorf("%s = %q, %v; want %q", src, out, err, want)
		}
	}
	for _, src := range []string{`{{ a[0 }}`, `{{ a["x ] }}`, `{{ a[] }}`} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) succeeded", src)
		}
	}
}
