package ui

import (
	"reflect"
	"testing"
)

func TestParseTextFilter(t *testing.T) {
	tests := []struct {
		in               string
		include, exclude []string
	}{
		{"", nil, nil},
		{"   ", nil, nil},
		{"Error", []string{"error"}, nil},
		{"~Choreographer", nil, []string{"choreographer"}},
		{"error  ~Choreographer ~gc", []string{"error"}, []string{"choreographer", "gc"}},
		{`"player started" ~"buffer size"`, []string{"player started"}, []string{"buffer size"}},
		{`"unclosed phrase`, []string{"unclosed phrase"}, nil},
		{"~", nil, nil},
		{"error ~", []string{"error"}, nil},
		{`\~home`, []string{"~home"}, nil},
		{"a~b", []string{"a~b"}, nil},
		{`""`, nil, nil},
	}
	for _, tt := range tests {
		got := parseTextFilter(tt.in)
		if !reflect.DeepEqual(got.include, tt.include) || !reflect.DeepEqual(got.exclude, tt.exclude) {
			t.Errorf("parseTextFilter(%q) = %+v; want include %q exclude %q", tt.in, got, tt.include, tt.exclude)
		}
	}
}

func TestTextFilterMatch(t *testing.T) {
	line := "10-21 12:00:00.000 1234 1234 E Player: playback ERROR in decoder"
	tests := []struct {
		filter string
		want   bool
	}{
		{"", true},
		{"error", true},
		{"error decoder", true},
		{"error encoder", false},
		{"~error", false},
		{"player ~choreographer", true},
		{`"playback error"`, true},
		{`"error playback"`, false},
		{"~encoder ~gc", true},
	}
	for _, tt := range tests {
		if got := parseTextFilter(tt.filter).match(line); got != tt.want {
			t.Errorf("filter %q match = %v, want %v", tt.filter, got, tt.want)
		}
	}
}
