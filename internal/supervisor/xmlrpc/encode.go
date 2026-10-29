// Package xmlrpc is the minimal XML-RPC codec supervisord needs: requests with
// string, int and bool params, and responses holding a single value or a fault.
package xmlrpc

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
)

// EncodeRequest writes a methodCall for method to w.
// Supported param types are string, int and bool.
func EncodeRequest(w io.Writer, method string, params ...any) error {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	if err := xml.EscapeText(&buf, []byte(method)); err != nil {
		return err
	}
	buf.WriteString(`</methodName><params>`)
	for i, p := range params {
		buf.WriteString(`<param><value>`)
		switch v := p.(type) {
		case string:
			buf.WriteString(`<string>`)
			if err := xml.EscapeText(&buf, []byte(v)); err != nil {
				return err
			}
			buf.WriteString(`</string>`)
		case int:
			buf.WriteString(`<int>` + strconv.Itoa(v) + `</int>`)
		case bool:
			b := "0"
			if v {
				b = "1"
			}
			buf.WriteString(`<boolean>` + b + `</boolean>`)
		default:
			return fmt.Errorf("xmlrpc: param %d: unsupported type %T", i, p)
		}
		buf.WriteString(`</value></param>`)
	}
	buf.WriteString(`</params></methodCall>`)
	_, err := w.Write(buf.Bytes())
	return err
}
