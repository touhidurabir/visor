package xmlrpc

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Fault is an XML-RPC fault response.
type Fault struct {
	Code   int
	String string
}

func (f *Fault) Error() string {
	return fmt.Sprintf("xmlrpc fault %d: %s", f.Code, f.String)
}

// DecodeResponse reads a methodResponse and returns its single value, or a
// *Fault if the server answered with one.
//
// Decoded values are int, bool, string, float64, []byte (base64), nil,
// []any (array) and map[string]any (struct).
func DecodeResponse(r io.Reader) (any, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	p := &parser{d: xml.NewDecoder(bytes.NewReader(sanitize(raw)))}
	v, err := p.response()
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			return nil, err
		}
		return nil, fmt.Errorf("xmlrpc: %w", err)
	}
	return v, nil
}

// supervisord copies log bytes into its XML unescaped, so ANSI colour codes
// and other control characters arrive raw, and Go's XML parser rejects them.
// Before parsing they are mapped to private-use code points (ctrlBase + byte),
// and restore maps them back in every piece of text. \r is mapped too, because
// an XML parser would otherwise normalise it to \n.
const ctrlBase = 0xF700

func mapped(c rune) bool {
	return c < 0x20 && c != '\t' && c != '\n'
}

func sanitize(raw []byte) []byte {
	raw = bytes.ToValidUTF8(raw, []byte("�"))
	var b bytes.Buffer
	b.Grow(len(raw))
	for len(raw) > 0 {
		c, size := utf8.DecodeRune(raw)
		switch {
		case mapped(c):
			b.WriteRune(ctrlBase + c)
		case c == 0xFFFE || c == 0xFFFF: // valid UTF-8, but not XML characters
			b.WriteRune(utf8.RuneError)
		default:
			b.Write(raw[:size])
		}
		raw = raw[size:]
	}
	return b.Bytes()
}

func restore(s string) string {
	if !strings.ContainsFunc(s, isMappedCtrl) {
		return s
	}
	return strings.Map(func(c rune) rune {
		if isMappedCtrl(c) {
			return c - ctrlBase
		}
		return c
	}, s)
}

func isMappedCtrl(c rune) bool {
	return c >= ctrlBase && c < ctrlBase+0x20 && mapped(c-ctrlBase)
}

type parser struct {
	d *xml.Decoder
}

// next returns the next start or end element, skipping the XML declaration,
// comments and whitespace between elements.
func (p *parser) next() (xml.Token, error) {
	for {
		tok, err := p.d.Token()
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement, xml.EndElement:
			return t, nil
		case xml.CharData:
			if strings.TrimSpace(restore(string(t))) != "" {
				return nil, fmt.Errorf("unexpected text %q", t)
			}
		}
	}
}

func (p *parser) expectStart(name string) error {
	tok, err := p.next()
	if err != nil {
		return err
	}
	if s, ok := tok.(xml.StartElement); !ok || s.Name.Local != name {
		return fmt.Errorf("expected <%s>, got %s", name, describe(tok))
	}
	return nil
}

func (p *parser) expectEnd(name string) error {
	tok, err := p.next()
	if err != nil {
		return err
	}
	if e, ok := tok.(xml.EndElement); !ok || e.Name.Local != name {
		return fmt.Errorf("expected </%s>, got %s", name, describe(tok))
	}
	return nil
}

func describe(tok xml.Token) string {
	switch t := tok.(type) {
	case xml.StartElement:
		return "<" + t.Name.Local + ">"
	case xml.EndElement:
		return "</" + t.Name.Local + ">"
	}
	return fmt.Sprintf("%T", tok)
}

func (p *parser) response() (any, error) {
	if err := p.expectStart("methodResponse"); err != nil {
		return nil, err
	}
	tok, err := p.next()
	if err != nil {
		return nil, err
	}
	start, ok := tok.(xml.StartElement)
	if !ok {
		return nil, fmt.Errorf("expected <params> or <fault>, got %s", describe(tok))
	}
	var v any
	switch start.Name.Local {
	case "params":
		if err := p.expectStart("param"); err != nil {
			return nil, err
		}
		if v, err = p.value(); err != nil {
			return nil, err
		}
		if err := p.expectEnd("param"); err != nil {
			return nil, err
		}
		if err := p.expectEnd("params"); err != nil {
			return nil, err
		}
	case "fault":
		fv, err := p.value()
		if err != nil {
			return nil, err
		}
		if err := p.expectEnd("fault"); err != nil {
			return nil, err
		}
		if err := p.expectEnd("methodResponse"); err != nil {
			return nil, err
		}
		return nil, toFault(fv)
	default:
		return nil, fmt.Errorf("expected <params> or <fault>, got <%s>", start.Name.Local)
	}
	if err := p.expectEnd("methodResponse"); err != nil {
		return nil, err
	}
	return v, nil
}

func toFault(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("fault is %T, not a struct", v)
	}
	code, ok := m["faultCode"].(int)
	if !ok {
		return errors.New("fault has no int faultCode")
	}
	s, ok := m["faultString"].(string)
	if !ok {
		return errors.New("fault has no string faultString")
	}
	return &Fault{Code: code, String: s}
}

// value parses a <value> element, starting with its opening tag.
func (p *parser) value() (any, error) {
	if err := p.expectStart("value"); err != nil {
		return nil, err
	}
	return p.valueBody()
}

// valueBody parses the content of a <value> whose opening tag was consumed,
// up to and including </value>. A value without a type element is a string.
func (p *parser) valueBody() (any, error) {
	var text strings.Builder
	var v any
	typed := false
	for {
		tok, err := p.d.Token()
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			text.WriteString(restore(string(t)))
		case xml.StartElement:
			if typed {
				return nil, errors.New("value has more than one type element")
			}
			typed = true
			if v, err = p.typed(t.Name.Local); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if !typed {
				return text.String(), nil
			}
			if strings.TrimSpace(text.String()) != "" {
				return nil, fmt.Errorf("unexpected text %q in value", text.String())
			}
			return v, nil
		}
	}
}

// typed parses a type element (<int>, <struct>, …) whose opening tag was
// consumed, up to and including its closing tag.
func (p *parser) typed(name string) (any, error) {
	switch name {
	case "array":
		return p.array()
	case "struct":
		return p.structure()
	case "nil":
		return nil, p.expectEnd("nil")
	}
	s, err := p.text()
	if err != nil {
		return nil, err
	}
	switch name {
	case "string":
		return s, nil
	case "int", "i4", "i8":
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("bad <%s>: %w", name, err)
		}
		return n, nil
	case "boolean":
		switch strings.TrimSpace(s) {
		case "1":
			return true, nil
		case "0":
			return false, nil
		}
		return nil, fmt.Errorf("bad <boolean> %q", s)
	case "double":
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("bad <double>: %w", err)
		}
		return f, nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
		if err != nil {
			return nil, fmt.Errorf("bad <base64>: %w", err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("unsupported type <%s>", name)
}

// text reads character data up to the closing tag of a scalar element.
func (p *parser) text() (string, error) {
	var b strings.Builder
	for {
		tok, err := p.d.Token()
		if err == io.EOF {
			return "", io.ErrUnexpectedEOF
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.WriteString(restore(string(t)))
		case xml.StartElement:
			return "", fmt.Errorf("unexpected <%s> in scalar", t.Name.Local)
		case xml.EndElement:
			return b.String(), nil
		}
	}
}

func (p *parser) array() (any, error) {
	if err := p.expectStart("data"); err != nil {
		return nil, err
	}
	items := []any{}
	for {
		tok, err := p.next()
		if err != nil {
			return nil, err
		}
		if s, ok := tok.(xml.StartElement); ok && s.Name.Local == "value" {
			v, err := p.valueBody()
			if err != nil {
				return nil, err
			}
			items = append(items, v)
			continue
		}
		if e, ok := tok.(xml.EndElement); ok && e.Name.Local == "data" {
			return items, p.expectEnd("array")
		}
		return nil, fmt.Errorf("expected <value> or </data>, got %s", describe(tok))
	}
}

func (p *parser) structure() (any, error) {
	m := map[string]any{}
	for {
		tok, err := p.next()
		if err != nil {
			return nil, err
		}
		if e, ok := tok.(xml.EndElement); ok && e.Name.Local == "struct" {
			return m, nil
		}
		if s, ok := tok.(xml.StartElement); !ok || s.Name.Local != "member" {
			return nil, fmt.Errorf("expected <member> or </struct>, got %s", describe(tok))
		}
		if err := p.expectStart("name"); err != nil {
			return nil, err
		}
		name, err := p.text()
		if err != nil {
			return nil, err
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		if err := p.expectEnd("member"); err != nil {
			return nil, err
		}
		m[name] = v
	}
}
