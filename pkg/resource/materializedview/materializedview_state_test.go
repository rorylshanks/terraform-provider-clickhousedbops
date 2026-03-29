package materializedview

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/pkg/resource/schemahelpers"
)

func TestSyncMaterializedViewStateAppliesToTableSignatureDrift(t *testing.T) {
	ctx := context.Background()
	toColumns, diags := schemahelpers.ColumnSignaturesValue(ctx, []dbops.Column{
		{Name: "team_id", Type: "UInt64", Nullable: false},
	})
	if diags.HasError() {
		t.Fatalf("ColumnSignaturesValue() diagnostics = %v", diags)
	}

	state := MaterializedViewResourceModel{
		ToTable:   types.StringValue("posthog.events_rollup"),
		ToColumns: toColumns,
		Query:     types.StringValue("SELECT team_id FROM posthog.events"),
	}

	remote := &dbops.MaterializedView{
		ToTable: "posthog.events_rollup",
		ToColumns: []dbops.Column{
			{Name: "team_id", Type: "UInt64", Nullable: false},
			{Name: "event_count", Type: "UInt64", Nullable: false},
		},
		Query: "SELECT team_id, count() AS event_count FROM posthog.events GROUP BY team_id",
	}

	diags = syncMaterializedViewState(ctx, &state, remote)
	if diags.HasError() {
		t.Fatalf("syncMaterializedViewState() diagnostics = %v", diags)
	}

	gotColumns, diags := schemahelpers.ExpandColumnSignatures(ctx, state.ToColumns)
	if diags.HasError() {
		t.Fatalf("ExpandColumnSignatures() diagnostics = %v", diags)
	}
	if len(gotColumns) != 2 {
		t.Fatalf("expected remote to_columns to replace drifted state, got %#v", gotColumns)
	}
	if state.Query.ValueString() != remote.Query {
		t.Fatalf("expected remote query to be applied, got %q", state.Query.ValueString())
	}
}

func TestSyncMaterializedViewStateSyncsPopulateFlag(t *testing.T) {
	ctx := context.Background()
	state := MaterializedViewResourceModel{
		Populate: types.BoolValue(true),
	}

	remote := &dbops.MaterializedView{
		Engine:   "MergeTree() ORDER BY id",
		Populate: false,
		Query:    "SELECT id FROM posthog.events",
	}

	diags := syncMaterializedViewState(ctx, &state, remote)
	if diags.HasError() {
		t.Fatalf("syncMaterializedViewState() diagnostics = %v", diags)
	}
	if state.Populate.ValueBool() {
		t.Fatal("expected populate to be reset from remote state")
	}
}
