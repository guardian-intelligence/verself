package main

import "testing"

func TestPublicationTableListQuotesIdentifiers(t *testing.T) {
	got := publicationTableList([]string{`simple`, `has"quote`})
	want := `"public"."simple", "public"."has""quote"`
	if got != want {
		t.Fatalf("publicationTableList() = %q, want %q", got, want)
	}
}

func TestQualifiedRegclassKeepsSchemaReadableAndQuotesTable(t *testing.T) {
	got := qualifiedRegclass("public", `has"quote`)
	want := `public."has""quote"`
	if got != want {
		t.Fatalf("qualifiedRegclass() = %q, want %q", got, want)
	}
}
