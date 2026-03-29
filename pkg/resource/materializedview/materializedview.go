package materializedview

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/pkg/resource/schemahelpers"
)

//go:embed materializedview.md
var materializedViewResourceDescription string

var (
	_ resource.Resource                     = &Resource{}
	_ resource.ResourceWithConfigure        = &Resource{}
	_ resource.ResourceWithConfigValidators = &Resource{}
	_ resource.ResourceWithValidateConfig   = &Resource{}
	_ resource.ResourceWithImportState      = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

type Resource struct {
	client dbops.Client
}

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_materialized_view"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := schemahelpers.CommonSchemaAttributes("materialized view")
	attrs["columns"] = schemahelpers.ColumnsAttribute("Optional inline materialized-view columns for engine-backed definitions.")
	attrs["engine"] = schema.StringAttribute{
		Optional:    true,
		Description: "Raw ClickHouse engine expression. Set this or to_table, but not both.",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	attrs["populate"] = schema.BoolAttribute{
		Optional:    true,
		Description: "Whether to append POPULATE to the CREATE MATERIALIZED VIEW statement",
		PlanModifiers: []planmodifier.Bool{
			boolplanmodifier.RequiresReplace(),
		},
	}
	attrs["to_table"] = schema.StringAttribute{
		Optional:    true,
		Description: "Destination table for TO-based materialized views. Usually this references clickhousedbops_table.<name>.qualified_name.",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	attrs["to_columns"] = schemahelpers.ColumnSignaturesAttribute("Optional destination signature appended after TO <table> (...). Only name, type, and nullable are supported there.")
	attrs["query"] = schema.StringAttribute{
		Required:    true,
		Description: "Raw SELECT query used by the materialized view definition",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	resp.Schema = schema.Schema{
		Attributes:          attrs,
		MarkdownDescription: materializedViewResourceDescription,
	}
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	r.client = req.ProviderData.(dbops.Client)
}

func (r *Resource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("engine"), path.MatchRoot("to_table")),
	}
}

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if req.Config.Raw.IsNull() {
		return
	}

	var config MaterializedViewResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !config.Populate.IsNull() && !config.Populate.IsUnknown() &&
		!config.ToTable.IsNull() && !config.ToTable.IsUnknown() &&
		config.Populate.ValueBool() {
		resp.Diagnostics.AddAttributeError(
			path.Root("populate"),
			"Invalid Attribute Combination",
			"'populate' can only be set for engine-backed materialized views and cannot be combined with 'to_table'.",
		)
	}
	if !config.ToTable.IsNull() && !config.ToTable.IsUnknown() && !config.Columns.IsNull() && !config.Columns.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("columns"),
			"Invalid Attribute Combination",
			"'columns' can only be set for engine-backed materialized views. Use 'to_columns' with 'to_table' instead.",
		)
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan MaterializedViewResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, diags := r.createMaterializedView(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MaterializedViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	view, err := r.client.GetMaterializedView(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer())
	if err != nil {
		resp.Diagnostics.AddError("Error reading materialized view", fmt.Sprintf("%+v\n", err))
		return
	}

	if view == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(syncMaterializedViewState(ctx, &state, view)...)
	if resp.Diagnostics.HasError() {
		return
	}

	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, view.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unexpected in-place materialized view update",
		"Materialized views are replacement-only resources. Terraform should plan a replacement instead of calling Update.",
	)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state MaterializedViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteMaterializedView(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer()); err != nil {
		resp.Diagnostics.AddError("Error deleting materialized view", fmt.Sprintf("%+v\n", err))
	}
}

func (r *Resource) createMaterializedView(ctx context.Context, plan MaterializedViewResourceModel) (*MaterializedViewResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	view, err := expandMaterializedViewModel(ctx, plan)
	diags.Append(schemahelpers.DiagnosticsFromErr("Invalid materialized view configuration", err)...)
	if diags.HasError() {
		return nil, diags
	}

	createdView, err := r.client.CreateMaterializedView(ctx, view, plan.ClusterName.ValueStringPointer())
	diags.Append(schemahelpers.DiagnosticsFromErr("Invalid materialized view configuration", err)...)
	if diags.HasError() {
		return nil, diags
	}
	if createdView == nil {
		diags.AddError("Error creating materialized view", "ClickHouse returned no metadata for the created materialized view.")
		return nil, diags
	}

	state := plan
	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, createdView.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)

	return &state, diags
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	schemahelpers.ImportSchemaObjectState(ctx, req, resp)
}

func syncMaterializedViewState(ctx context.Context, state *MaterializedViewResourceModel, view *dbops.MaterializedView) diag.Diagnostics {
	var diags diag.Diagnostics

	if strings.TrimSpace(view.Query) != "" {
		state.Query = types.StringValue(view.Query)
	}

	if strings.TrimSpace(view.Engine) != "" {
		state.Engine = types.StringValue(view.Engine)
	}

	if strings.TrimSpace(view.ToTable) != "" {
		state.ToTable = types.StringValue(view.ToTable)
	}

	if view.Populate || (!state.Populate.IsNull() && !state.Populate.IsUnknown()) {
		state.Populate = types.BoolValue(view.Populate)
	}

	// Sync columns for engine-backed materialized views
	if len(view.Columns) > 0 || (!state.Columns.IsNull() && !state.Columns.IsUnknown()) {
		columns, columnDiags := schemahelpers.ColumnsValue(ctx, view.Columns)
		diags.Append(columnDiags...)
		if diags.HasError() {
			return diags
		}
		state.Columns = columns
	}

	// Sync to_columns for TO-based materialized views
	if len(view.ToColumns) > 0 || (!state.ToColumns.IsNull() && !state.ToColumns.IsUnknown()) {
		toColumns, columnDiags := schemahelpers.ColumnSignaturesValue(ctx, view.ToColumns)
		diags.Append(columnDiags...)
		if diags.HasError() {
			return diags
		}
		state.ToColumns = toColumns
	}

	return diags
}

func expandMaterializedViewModel(ctx context.Context, plan MaterializedViewResourceModel) (dbops.MaterializedView, error) {
	columns, diags := schemahelpers.ExpandColumns(ctx, plan.Columns)
	if diags.HasError() {
		return dbops.MaterializedView{}, schemahelpers.DiagnosticsError(diags)
	}

	toColumns, diags := schemahelpers.ExpandColumnSignatures(ctx, plan.ToColumns)
	if diags.HasError() {
		return dbops.MaterializedView{}, schemahelpers.DiagnosticsError(diags)
	}

	populate := false
	if !plan.Populate.IsNull() {
		populate = plan.Populate.ValueBool()
	}

	return dbops.MaterializedView{
		Database:  plan.Database.ValueString(),
		Name:      plan.Name.ValueString(),
		Columns:   columns,
		Engine:    plan.Engine.ValueString(),
		Populate:  populate,
		ToTable:   plan.ToTable.ValueString(),
		ToColumns: toColumns,
		Query:     plan.Query.ValueString(),
	}, nil
}
