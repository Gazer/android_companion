package plugin

import "testing"

func TestCompilePatternShared(t *testing.T) {
	// int placeholder
	re, err := CompilePattern("sessionId = {%d}, screenNumber = {%d}")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	m := re.FindStringSubmatch("Updated state: sessionId = 42, screenNumber = 3")
	if m == nil {
		t.Fatal("no match")
	}
	if m[1] != "42" || m[2] != "3" {
		t.Errorf("captures = %v, want [42 3]", m[1:])
	}

	// string placeholder
	re2, err := CompilePattern("user {%s} logged in")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	m2 := re2.FindStringSubmatch("user john logged in")
	if m2 == nil || m2[1] != "john" {
		t.Errorf("string capture failed: %v", m2)
	}

	// literal text with special regex chars must be quoted
	re3, err := CompilePattern("value: {%d} (exact)")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if re3.FindStringSubmatch("value: 5 (exact)") == nil {
		t.Error("literal parens not matched")
	}
	if re3.FindStringSubmatch("value: 5 XexactY") != nil {
		t.Error("parens should be literal, not regex groups")
	}
}
