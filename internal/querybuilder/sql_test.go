package querybuilder

import (
	"testing"
)

func TestSplitTopLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		separator byte
		want      []string
		wantErr   bool
	}{
		{
			name:      "simple comma split",
			input:     "a, b, c",
			separator: ',',
			want:      []string{"a", " b", " c"},
		},
		{
			name:      "equals separator",
			input:     "index_granularity = 8192",
			separator: '=',
			want:      []string{"index_granularity ", " 8192"},
		},
		{
			name:      "comma inside parentheses is not a split point",
			input:     "toYYYYMM(a, b), c",
			separator: ',',
			want:      []string{"toYYYYMM(a, b)", " c"},
		},
		{
			name:      "comma inside single quotes is not a split point",
			input:     "path = 'a,b', retries = 3",
			separator: ',',
			want:      []string{"path = 'a,b'", " retries = 3"},
		},
		{
			name:      "comma inside backticks is not a split point",
			input:     "`col,name` UInt64, `other` String",
			separator: ',',
			want:      []string{"`col,name` UInt64", " `other` String"},
		},
		{
			name:      "backslash-escaped quote preserves quoting",
			input:     "a = 'it\\'s,here', b = 1",
			separator: ',',
			want:      []string{"a = 'it\\'s,here'", " b = 1"},
		},
		{
			name:      "doubled single quote preserves quoting",
			input:     "a = 'team''s,thing', b = 1",
			separator: ',',
			want:      []string{"a = 'team''s,thing'", " b = 1"},
		},
		{
			name:      "no separator returns single part",
			input:     "just a value",
			separator: ',',
			want:      []string{"just a value"},
		},
		{
			name:      "empty string returns single empty part",
			input:     "",
			separator: ',',
			want:      []string{""},
		},
		{
			name:      "nested parens with equals",
			input:     "MergeTree(col1, col2) = something",
			separator: '=',
			want:      []string{"MergeTree(col1, col2) ", " something"},
		},
		{
			name:      "unterminated single quote errors",
			input:     "a = 'unterminated",
			separator: ',',
			wantErr:   true,
		},
		{
			name:      "unterminated parenthesis errors",
			input:     "Nullable(String",
			separator: ',',
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitTopLevel(tt.input, tt.separator)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d parts %#v, want %d parts %#v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("part[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitTopLevelCSVTrimsAndFiltersEmpty(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "trims whitespace from parts",
			input: " a , b , c ",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "drops empty parts from trailing comma",
			input: "a, b, ",
			want:  []string{"a", "b"},
		},
		{
			name:  "empty input returns empty slice",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only returns empty slice",
			input: "   ",
			want:  nil,
		},
		{
			name:  "preserves content inside parens",
			input: "Nullable(String), UInt64",
			want:  []string{"Nullable(String)", "UInt64"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitTopLevelCSV(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d parts %#v, want %d parts %#v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("part[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitTopLevelCSVPropagatesError(t *testing.T) {
	_, err := SplitTopLevelCSV("a = 'unterminated, b")
	if err == nil {
		t.Fatal("expected error for unterminated quote, got nil")
	}
}
