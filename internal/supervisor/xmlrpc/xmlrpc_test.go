package xmlrpc

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeRequest(t *testing.T) {
	var buf bytes.Buffer
	err := EncodeRequest(&buf, "supervisor.tailProcessStdoutLog", "web:web_00", 0, -1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0"?>` +
		`<methodCall><methodName>supervisor.tailProcessStdoutLog</methodName><params>` +
		`<param><value><string>web:web_00</string></value></param>` +
		`<param><value><int>0</int></value></param>` +
		`<param><value><int>-1</int></value></param>` +
		`<param><value><boolean>1</boolean></value></param>` +
		`<param><value><boolean>0</boolean></value></param>` +
		`</params></methodCall>`
	if got := buf.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestEncodeRequestNoParams(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeRequest(&buf, "supervisor.getState"); err != nil {
		t.Fatal(err)
	}
	want := `<?xml version="1.0"?><methodCall><methodName>supervisor.getState</methodName><params></params></methodCall>`
	if got := buf.String(); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestEncodeRequestEscapesStrings(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeRequest(&buf, "m", `a<b>&"c'`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `<string>a&lt;b&gt;&amp;&#34;c&#39;</string>`) {
		t.Errorf("string not escaped: %s", buf.String())
	}
}

func TestEncodeRequestRejectsUnsupportedTypes(t *testing.T) {
	for _, p := range []any{1.5, []string{"a"}, struct{}{}, nil} {
		if err := EncodeRequest(&bytes.Buffer{}, "m", p); err == nil {
			t.Errorf("param %#v: want error, got nil", p)
		}
	}
}

func response(value string) string {
	return `<?xml version='1.0'?><methodResponse><params><param><value>` + value + `</value></param></params></methodResponse>`
}

func TestDecodeScalars(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  any
	}{
		{"int", `<int>42</int>`, 42},
		{"i4", `<i4>-7</i4>`, -7},
		{"int with whitespace", "<int>\n 42 \n</int>", 42},
		{"boolean true", `<boolean>1</boolean>`, true},
		{"boolean false", `<boolean>0</boolean>`, false},
		{"string", `<string>RUNNING</string>`, "RUNNING"},
		{"empty string", `<string></string>`, ""},
		{"untyped value is a string", `abc`, "abc"},
		{"empty untyped value", ``, ""},
		{"entities", `<string>&lt;b&gt; &amp; ü 🚀</string>`, "<b> & ü 🚀"},
		{"double", `<double>1.5</double>`, 1.5},
		{"base64", `<base64>aGk=</base64>`, []byte("hi")},
		{"nil", `<nil/>`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := DecodeResponse(strings.NewReader(response(c.value)))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestDecodeNested(t *testing.T) {
	value := `<array><data>
<value><struct>
<member><name>name</name><value><string>web</string></value></member>
<member><name>ids</name><value><array><data><value><int>1</int></value><value><int>2</int></value></data></array></value></member>
<member><name>empty</name><value><array><data></data></array></value></member>
</struct></value>
<value><boolean>1</boolean></value>
</data></array>`
	got, err := DecodeResponse(strings.NewReader(response(value)))
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"name": "web", "ids": []any{1, 2}, "empty": []any{}},
		true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeGetAllProcessInfoFixture(t *testing.T) {
	got, err := DecodeResponse(bytes.NewReader(readFixture(t, "getAllProcessInfo.xml")))
	if err != nil {
		t.Fatal(err)
	}
	procs, ok := got.([]any)
	if !ok || len(procs) != 3 {
		t.Fatalf("want 3 processes, got %#v", got)
	}
	web := procs[0].(map[string]any)
	if web["name"] != "web" || web["state"] != 20 || web["statename"] != "RUNNING" || web["pid"] != 4242 {
		t.Errorf("unexpected first process: %#v", web)
	}
	if len(web) != 14 {
		t.Errorf("want 14 fields per process, got %d", len(web))
	}
	if reverb := procs[1].(map[string]any); reverb["statename"] != "FATAL" || reverb["pid"] != 0 {
		t.Errorf("unexpected second process: %#v", reverb)
	}
}

func TestDecodeFaultFixture(t *testing.T) {
	got, err := DecodeResponse(bytes.NewReader(readFixture(t, "fault.xml")))
	if got != nil {
		t.Errorf("want nil value with a fault, got %#v", got)
	}
	var f *Fault
	if !errors.As(err, &f) {
		t.Fatalf("want *Fault, got %T %v", err, err)
	}
	if f.Code != 10 || f.String != "BAD_NAME: nope" {
		t.Errorf("got %+v", f)
	}
	if err.Error() != "xmlrpc fault 10: BAD_NAME: nope" {
		t.Errorf("Error() = %q", err.Error())
	}
}

// supervisord writes log bytes into the XML unescaped, so ANSI colour codes
// arrive as raw control characters that XML 1.0 forbids. They must survive
// decoding byte-for-byte, as must \r (which an XML parser would turn into \n).
func TestDecodePreservesControlCharacters(t *testing.T) {
	got, err := DecodeResponse(bytes.NewReader(readFixture(t, "tailProcessStdoutLog.xml")))
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		"\x1b[32m  INFO \x1b[39m Server running…\r\nprogress 10%\rprogress 99%\n<b>&amp; ünïcødé 🚀\n",
		1234,
		false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestDecodeControlCharactersEverywhere(t *testing.T) {
	var raw strings.Builder
	for c := byte(0); c < 0x20; c++ {
		raw.WriteByte(c)
	}
	got, err := DecodeResponse(strings.NewReader(response("<string>" + raw.String() + "</string>")))
	if err != nil {
		t.Fatal(err)
	}
	if got != raw.String() {
		t.Errorf("got %q, want %q", got, raw.String())
	}
}

func TestDecodeInvalidUTF8(t *testing.T) {
	got, err := DecodeResponse(strings.NewReader(response("<string>ok \xff done</string>")))
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok � done" {
		t.Errorf("got %q", got)
	}
}

func TestDecodeMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":             ``,
		"not xml":           `hello`,
		"truncated":         `<?xml version='1.0'?><methodResponse><params><param><value><int>4`,
		"wrong root":        `<methodCall></methodCall>`,
		"no params":         `<methodResponse><params></params></methodResponse>`,
		"two params":        `<methodResponse><params><param><value>a</value></param><param><value>b</value></param></params></methodResponse>`,
		"bad int":           response(`<int>x</int>`),
		"int overflow":      response(`<int>99999999999999999999</int>`),
		"bad boolean":       response(`<boolean>2</boolean>`),
		"bad double":        response(`<double>nan-ish</double>`),
		"bad base64":        response(`<base64>!!!</base64>`),
		"unknown type":      response(`<foo>1</foo>`),
		"two typed values":  response(`<int>1</int><int>2</int>`),
		"member no name":    response(`<struct><member><value>a</value></member></struct>`),
		"member no value":   response(`<struct><member><name>a</name></member></struct>`),
		"array no data":     response(`<array></array>`),
		"fault not struct":  `<methodResponse><fault><value><int>1</int></value></fault></methodResponse>`,
		"fault bad code":    `<methodResponse><fault><value><struct><member><name>faultCode</name><value>x</value></member><member><name>faultString</name><value>s</value></member></struct></value></fault></methodResponse>`,
		"fault no string":   `<methodResponse><fault><value><struct><member><name>faultCode</name><value><int>1</int></value></member></struct></value></fault></methodResponse>`,
		"text outside tags": `<methodResponse>junk<params><param><value>a</value></param></params></methodResponse>`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeResponse(strings.NewReader(doc)); err == nil {
				t.Error("want error, got nil")
			}
		})
	}
}

func FuzzDecodeResponse(f *testing.F) {
	for _, name := range []string{"getAllProcessInfo.xml", "fault.xml", "tailProcessStdoutLog.xml"} {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte(response(`<struct><member><name>a</name><value><array><data/></array></value></member></struct>`)))
	f.Fuzz(func(t *testing.T, doc []byte) {
		_, _ = DecodeResponse(bytes.NewReader(doc)) // must never panic
	})
}
