package integrations

import (
	"reflect"
	"testing"
)

func TestDecodeJSONC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]any
	}{
		{
			name:  "plain json is unaffected",
			input: `{"plugin":["a"]}`,
			want:  map[string]any{"plugin": []any{"a"}},
		},
		{
			name: "line comments",
			input: `{
  // which model to use
  "model": "sonnet" // trailing
}`,
			want: map[string]any{"model": "sonnet"},
		},
		{
			name: "block comment spanning lines",
			input: `{
  /* a note
     over two lines */
  "model": "sonnet"
}`,
			want: map[string]any{"model": "sonnet"},
		},
		{
			name:  "trailing comma in object and array",
			input: "{\n  \"plugin\": [\n    \"a\",\n  ],\n}",
			want:  map[string]any{"plugin": []any{"a"}},
		},
		{
			name:  "comment-like sequences inside strings survive",
			input: `{"plugin":["file:///tmp/a.ts","https://example.com/x","/*not a comment*/"]}`,
			want: map[string]any{"plugin": []any{
				"file:///tmp/a.ts",
				"https://example.com/x",
				"/*not a comment*/",
			}},
		},
		{
			name:  "escaped quote before a comment marker",
			input: `{"a":"say \"hi\"" // done` + "\n}",
			want:  map[string]any{"a": `say "hi"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			if err := decodeJSONC([]byte(tt.input), &got); err != nil {
				t.Fatalf("decodeJSONC() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("decodeJSONC() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeJSONCRejectsInvalid(t *testing.T) {
	var got map[string]any
	if err := decodeJSONC([]byte(`{"a": }`), &got); err == nil {
		t.Fatal("decodeJSONC() error = nil, want a decode error")
	}
}
