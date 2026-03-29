package materializedview

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

func TestSchemaToColumnsOnlyExposeSignatureFields(t *testing.T) {
	var resp resource.SchemaResponse
	(&Resource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	attribute, ok := resp.Schema.Attributes["to_columns"]
	if !ok {
		t.Fatal("expected to_columns attribute to be present")
	}

	toColumns, ok := attribute.(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("expected to_columns to be a list nested attribute, got %T", attribute)
	}

	expected := map[string]struct{}{
		"name":     {},
		"type":     {},
		"nullable": {},
	}
	if len(toColumns.NestedObject.Attributes) != len(expected) {
		t.Fatalf("expected only signature fields, got %#v", toColumns.NestedObject.Attributes)
	}

	for name := range expected {
		if _, ok := toColumns.NestedObject.Attributes[name]; !ok {
			t.Fatalf("expected to_columns schema to include %q", name)
		}
	}

	for _, name := range []string{"comment", "default_expression", "materialized_expression", "alias_expression"} {
		if _, ok := toColumns.NestedObject.Attributes[name]; ok {
			t.Fatalf("did not expect to_columns schema to include %q", name)
		}
	}
}
