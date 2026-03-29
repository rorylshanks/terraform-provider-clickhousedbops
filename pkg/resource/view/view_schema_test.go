package view

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

func TestSchemaColumnsOnlyExposeSignatureFields(t *testing.T) {
	var resp resource.SchemaResponse
	(&Resource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	attribute, ok := resp.Schema.Attributes["columns"]
	if !ok {
		t.Fatal("expected columns attribute to be present")
	}

	columns, ok := attribute.(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("expected columns to be a list nested attribute, got %T", attribute)
	}

	expected := map[string]struct{}{
		"name":     {},
		"type":     {},
		"nullable": {},
	}
	if len(columns.NestedObject.Attributes) != len(expected) {
		t.Fatalf("expected only signature fields, got %#v", columns.NestedObject.Attributes)
	}

	for name := range expected {
		if _, ok := columns.NestedObject.Attributes[name]; !ok {
			t.Fatalf("expected columns schema to include %q", name)
		}
	}

	for _, name := range []string{"comment", "default_expression", "materialized_expression", "alias_expression"} {
		if _, ok := columns.NestedObject.Attributes[name]; ok {
			t.Fatalf("did not expect columns schema to include %q", name)
		}
	}
}
