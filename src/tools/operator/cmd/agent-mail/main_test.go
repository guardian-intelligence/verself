package main

import (
	"strings"
	"testing"
)

func TestRenderHTMLSubstitutesAndEscapes(t *testing.T) {
	out, err := renderHTML("Build <green>", "line one\n<script>alert(1)</script>")
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("unsubstituted template action remains: %q", out)
	}
	if !strings.Contains(out, "Build &lt;green&gt;") {
		t.Fatalf("subject not HTML-escaped: %q", out)
	}
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Fatalf("body HTML not escaped: %q", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("expected escaped body marker: %q", out)
	}
}

func TestRenderTextIncludesSubjectAndBody(t *testing.T) {
	out := renderText("subject line", "body line")
	if !strings.Contains(out, "subject line") || !strings.Contains(out, "body line") {
		t.Fatalf("text body missing content: %q", out)
	}
}

func TestContentKeyStableAndDistinct(t *testing.T) {
	a := contentKey("a@x", "b@y", "s", "b")
	if a != contentKey("a@x", "b@y", "s", "b") {
		t.Fatal("content key is not stable for identical inputs")
	}
	if a == contentKey("a@x", "b@y", "s", "different") {
		t.Fatal("content key did not change with body")
	}
	if !strings.HasPrefix(a, "agent-mail:") {
		t.Fatalf("unexpected key prefix: %q", a)
	}
}
