package config

import "testing"

func TestExpandParameterizedURI(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want []string
	}{
		{
			name: "numeric range",
			uri:  "http://example.com/asset-[1-3].bin",
			want: []string{
				"http://example.com/asset-1.bin",
				"http://example.com/asset-2.bin",
				"http://example.com/asset-3.bin",
			},
		},
		{
			name: "padded numeric range with step",
			uri:  "http://example.com/asset-[001-005:2].bin",
			want: []string{
				"http://example.com/asset-001.bin",
				"http://example.com/asset-003.bin",
				"http://example.com/asset-005.bin",
			},
		},
		{
			name: "choice and alpha range",
			uri:  "http://{a,b}.example.com/[x-z]",
			want: []string{
				"http://a.example.com/x",
				"http://a.example.com/y",
				"http://a.example.com/z",
				"http://b.example.com/x",
				"http://b.example.com/y",
				"http://b.example.com/z",
			},
		},
		{
			name: "no parameter",
			uri:  "http://example.com/file.bin",
			want: []string{"http://example.com/file.bin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandParameterizedURI(tt.uri)
			if err != nil {
				t.Fatalf("ExpandParameterizedURI() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("entry %d = %q, want %q; all=%v", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestExpandParameterizedURIRejectsMalformedInput(t *testing.T) {
	tests := []string{
		"http://example.com/{a,b",
		"http://example.com/[1-2",
		"http://example.com/[1-2:0]",
		"http://example.com/[1-x]",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := ExpandParameterizedURI(input); err == nil {
				t.Fatal("ExpandParameterizedURI() error = nil, want error")
			}
		})
	}
}
