package httpx

import "testing"

func TestErrorEnvelopeByteExact(t *testing.T) {
	got := ErrorEnvelope("model gpt-4 not allowed through proxy")
	want := `{"type": "error", "error": {"message": "model gpt-4 not allowed through proxy"}}`
	if got != want {
		t.Fatalf("错误体不一致：\n got=%s\nwant=%s", got, want)
	}
}

func TestErrorEnvelopeNoneModel(t *testing.T) {
	got := ErrorEnvelope("model None not allowed through proxy")
	want := `{"type": "error", "error": {"message": "model None not allowed through proxy"}}`
	if got != want {
		t.Fatalf("错误体不一致：\n got=%s\nwant=%s", got, want)
	}
}

func TestQuoteMatchesPythonEscaping(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`a"b`, `"a\"b"`},
		{`a\b`, `"a\\b"`},
		{"a\nb", `"a\nb"`},
		{"a\tb", `"a\tb"`},
		{"caf\u00e9", `"caf\u00e9"`},
		{"\U0001F600", `"\ud83d\ude00"`},
		{"\x7f", `"\u007f"`},
		{"\x01", `"\u0001"`},
		{"keep/this", `"keep/this"`},
	}
	for _, c := range cases {
		if got := Quote(c.in); got != c.want {
			t.Errorf("Quote(%q) = %s，期望 %s", c.in, got, c.want)
		}
	}
}

func TestPyStr(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "None"},
		{true, "True"},
		{false, "False"},
		{"muse-spark-1.3-contributor", "muse-spark-1.3-contributor"},
	}
	for _, c := range cases {
		if got := PyStr(c.in); got != c.want {
			t.Errorf("PyStr(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
