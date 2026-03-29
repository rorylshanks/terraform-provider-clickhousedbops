package view

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/pkg/resource/schemahelpers"
)

func TestSyncViewStateAppliesRemoteDefinitionDrift(t *testing.T) {
	ctx := context.Background()
	columns, diags := schemahelpers.ColumnSignaturesValue(ctx, []dbops.Column{
		{Name: "team_id", Type: "UInt64", Nullable: false},
	})
	if diags.HasError() {
		t.Fatalf("ColumnSignaturesValue() diagnostics = %v", diags)
	}

	state := ViewResourceModel{
		Columns: columns,
		Query:   types.StringValue("SELECT team_id FROM posthog.events"),
	}

	remote := &dbops.View{
		Columns: []dbops.Column{
			{Name: "team_id", Type: "UInt64", Nullable: false},
			{Name: "event_count", Type: "UInt64", Nullable: false},
		},
		Query: "SELECT team_id, count() AS event_count FROM posthog.events GROUP BY team_id",
	}

	diags = syncViewState(ctx, &state, remote)
	if diags.HasError() {
		t.Fatalf("syncViewState() diagnostics = %v", diags)
	}

	if state.Query.ValueString() != remote.Query {
		t.Fatalf("expected remote query to be applied, got %q", state.Query.ValueString())
	}

	gotColumns, diags := schemahelpers.ExpandColumnSignatures(ctx, state.Columns)
	if diags.HasError() {
		t.Fatalf("ExpandColumnSignatures() diagnostics = %v", diags)
	}
	if len(gotColumns) != 2 {
		t.Fatalf("expected remote columns to replace drifted state, got %#v", gotColumns)
	}
}

func TestSyncViewStateClearsManagedColumnsWhenRemoteSignatureIsRemoved(t *testing.T) {
	ctx := context.Background()
	columns, diags := schemahelpers.ColumnSignaturesValue(ctx, []dbops.Column{
		{Name: "team_id", Type: "UInt64", Nullable: false},
	})
	if diags.HasError() {
		t.Fatalf("ColumnSignaturesValue() diagnostics = %v", diags)
	}

	state := ViewResourceModel{
		Columns: columns,
		Query:   types.StringValue("SELECT team_id FROM posthog.events"),
	}

	remote := &dbops.View{
		Query: "SELECT team_id FROM posthog.events",
	}

	diags = syncViewState(ctx, &state, remote)
	if diags.HasError() {
		t.Fatalf("syncViewState() diagnostics = %v", diags)
	}
	if !state.Columns.IsNull() {
		t.Fatalf("expected managed columns to be cleared when remote signature is absent, got %#v", state.Columns)
	}
}
