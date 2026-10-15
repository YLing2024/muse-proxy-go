package httpx

import (
	"encoding/json"
	"math"
	"testing"
)

// TestPyDecodeErrorsMatchPython 用真实输入向量锁定 400 文案，逐字节与
// CPython 3.13 json.loads 的 JSONDecodeError 一致（含行列与 char 偏移）。
func TestPyDecodeErrorsMatchPython(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{not json`, "Expecting property name enclosed in double quotes: line 1 column 2 (char 1)"},
		{`{"a"`, "Expecting ':' delimiter: line 1 column 5 (char 4)"},
		{`{"a":1} extra`, "Extra data: line 1 column 9 (char 8)"},
		{`not json`, "Expecting value: line 1 column 1 (char 0)"},
		{``, "Expecting value: line 1 column 1 (char 0)"},
		{`{"a":}`, "Expecting value: line 1 column 6 (char 5)"},
		{`{"a":1 "b":2}`, "Expecting ',' delimiter: line 1 column 8 (char 7)"},
		{`{"a":"unterminated`, "Unterminated string starting at: line 1 column 6 (char 5)"},
		{"{\"a\":\"ctrl\x01\"}", "Invalid control character at: line 1 column 11 (char 10)"},
		{"{\"a\":1}\n{\"b\":2}", "Extra data: line 2 column 1 (char 8)"},
		// 多字节输入按 code point 计位，而非字节偏移。
		{`{"é":}`, "Expecting value: line 1 column 6 (char 5)"},
		{"{\"日\":1 \"x\":2}", "Expecting ',' delimiter: line 1 column 8 (char 7)"},
	}
	for _, c := range cases {
		_, err := PyDecode([]byte(c.in))
		if err == nil {
			t.Errorf("PyDecode(%q) 期望报错，实际通过", c.in)
			continue
		}
		if got := err.Error(); got != c.want {
			t.Errorf("PyDecode(%q):\n got=%s\nwant=%s", c.in, got, c.want)
		}
	}
}

// TestPyDecodeExtraDataVector 覆盖需求书点名的尾部多余内容向量。
func TestPyDecodeExtraDataVector(t *testing.T) {
	_, err := PyDecode([]byte(`{"a":1} extra`))
	if err == nil || err.Error() != "Extra data: line 1 column 9 (char 8)" {
		t.Fatalf("Extra data 文案不符：%v", err)
	}
}

// TestPyAttrErrorMatchesPython 覆盖"解析成功但非对象"的 AttributeError 文案。
func TestPyAttrErrorMatchesPython(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`[1,2]`, "'list' object has no attribute 'get'"},
		{`"str"`, "'str' object has no attribute 'get'"},
		{`123`, "'int' object has no attribute 'get'"},
		{`1.5`, "'float' object has no attribute 'get'"},
		{`true`, "'bool' object has no attribute 'get'"},
		{`null`, "'NoneType' object has no attribute 'get'"},
	}
	for _, c := range cases {
		v, err := PyDecode([]byte(c.in))
		if err != nil {
			t.Errorf("PyDecode(%q) 不应报错：%v", c.in, err)
			continue
		}
		if got := PyAttrError(v); got != c.want {
			t.Errorf("PyAttrError(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestPyDecodeValues 校验与 json.loads 等价的取值形态。
func TestPyDecodeValues(t *testing.T) {
	v, err := PyDecode([]byte(`{"model":"muse-spark-1.3-contributor","n":1,"f":2.5,"b":true,"z":null,"a":[1,2],"o":{}}`))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("顶层应为对象，得到 %T", v)
	}
	if obj["model"] != "muse-spark-1.3-contributor" {
		t.Errorf("model 取值错误：%v", obj["model"])
	}
	if _, ok := obj["n"].(json.Number); !ok {
		t.Errorf("整数应保存为 json.Number，得到 %T", obj["n"])
	}
	if f, ok := obj["f"].(float64); !ok || f != 2.5 {
		t.Errorf("实数应为 float64 2.5，得到 %T %v", obj["f"], obj["f"])
	}
	if obj["b"] != true || obj["z"] != nil {
		t.Errorf("bool/null 取值错误")
	}
	if _, ok := obj["a"].([]any); !ok {
		t.Errorf("数组应为 []any，得到 %T", obj["a"])
	}
}

// TestPyDecodeConstants 校验 NaN/Infinity 取值，以及 PyStr 的 Python 化输出。
func TestPyDecodeConstants(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{`{"model": NaN}`, "nan"},
		{`{"model": Infinity}`, "inf"},
		{`{"model": -Infinity}`, "-inf"},
		{`{"model": 1.0}`, "1.0"},
		{`{"model": 1e2}`, "100.0"},
	} {
		v, err := PyDecode([]byte(c.in))
		if err != nil {
			t.Fatalf("PyDecode(%q) 失败：%v", c.in, err)
		}
		obj := v.(map[string]any)
		if got := PyStr(obj["model"]); got != c.want {
			t.Errorf("PyStr(%q model)=%q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestPyDecodeNaNIsNaN 双保险：确认常量确实解析为 math.NaN。
func TestPyDecodeNaNIsNaN(t *testing.T) {
	v, err := PyDecode([]byte(`{"model": NaN}`))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	f, ok := v.(map[string]any)["model"].(float64)
	if !ok || !math.IsNaN(f) {
		t.Fatalf("NaN 应解析为 float64 NaN，得到 %T", v.(map[string]any)["model"])
	}
}

// TestPyDecodeInvalidUTF8MatchesPython 覆盖非 UTF-8 字节的解码文案。
func TestPyDecodeInvalidUTF8MatchesPython(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"\xff", "'utf-8' codec can't decode byte 0xff in position 0: invalid start byte"},
		{"\xc3", "'utf-8' codec can't decode byte 0xc3 in position 0: unexpected end of data"},
		{"\xe4\xbd", "'utf-8' codec can't decode bytes in position 0-1: unexpected end of data"},
		{"\xc3(", "'utf-8' codec can't decode byte 0xc3 in position 0: invalid continuation byte"},
		{"{\"a\":\xe4\xbd}", "'utf-8' codec can't decode bytes in position 5-6: invalid continuation byte"},
	}
	for _, c := range cases {
		_, err := PyDecode([]byte(c.in))
		if err == nil || err.Error() != c.want {
			t.Errorf("PyDecode(%q):\n got=%v\nwant=%s", c.in, err, c.want)
		}
	}
}
