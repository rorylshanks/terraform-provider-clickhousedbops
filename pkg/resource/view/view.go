package view

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/pkg/resource/schemahelpers"
)

//go:embed view.md
var viewResourceDescription string

var (
	_ resource.Resource                = &Resource{}
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

type Resource struct {
	client dbops.Client
}

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_view"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := schemahelpers.CommonSchemaAttributes("view")
	attrs["columns"] = schemahelpers.ColumnSignaturesAttribute("Optional view signature. Only name, type, and nullable are included in the CREATE VIEW signature.")
	attrs["query"] = schema.StringAttribute{
		Required:    true,
		Description: "Raw SELECT query used by the view definition",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	resp.Schema = schema.Schema{
		Attributes:          attrs,
		MarkdownDescription: viewResourceDescription,
	}
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	r.client = req.ProviderData.(dbops.Client)
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ViewResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, diags := r.createView(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	view, err := r.client.GetView(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer())
	if err != nil {
		resp.Diagnostics.AddError("Error reading view", fmt.Sprintf("%+v\n", err))
		return
	}

	if view == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(syncViewState(ctx, &state, view)...)
	if resp.Diagnostics.HasError() {
		return
	}

	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, view.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unexpected in-place view update",
		"Views are replacement-only resources. Terraform should plan a replacement instead of calling Update.",
	)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteView(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer()); err != nil {
		resp.Diagnostics.AddError("Error deleting view", fmt.Sprintf("%+v\n", err))
	}
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	schemahelpers.ImportSchemaObjectState(ctx, req, resp)
}

func (r *Resource) createView(ctx context.Context, plan ViewResourceModel) (*ViewResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	view, err := expandViewModel(ctx, plan)
	diags.Append(schemahelpers.DiagnosticsFromErr("Invalid view configuration", err)...)
	if diags.HasError() {
		return nil, diags
	}

	createdView, err := r.client.CreateView(ctx, view, plan.ClusterName.ValueStringPointer())
	diags.Append(schemahelpers.DiagnosticsFromErr("Error creating view", err)...)
	if diags.HasError() {
		return nil, diags
	}
	if createdView == nil {
		diags.AddError("Error creating view", "ClickHouse returned no metadata for the created view.")
		return nil, diags
	}

	state := plan
	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, createdView.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)

	return &state, diags
}

func expandViewModel(ctx context.Context, plan ViewResourceModel) (dbops.View, error) {
	columns, diags := schemahelpers.ExpandColumnSignatures(ctx, plan.Columns)
	if diags.HasError() {
		return dbops.View{}, schemahelpers.DiagnosticsError(diags)
	}

	return dbops.View{
		Database: plan.Database.ValueString(),
		Name:     plan.Name.ValueString(),
		Columns:  columns,
		Query:    plan.Query.ValueString(),
	}, nil
}

func syncViewState(ctx context.Context, state *ViewResourceModel, view *dbops.View) diag.Diagnostics {
	var diags diag.Diagnostics

	if strings.TrimSpace(view.Query) != "" {
		state.Query = types.StringValue(view.Query)
	} else if !state.Query.IsNull() && !state.Query.IsUnknown() {
		state.Query = types.StringNull()
	}

	currentColumns, columnDiags := schemahelpers.ExpandColumnSignatures(ctx, state.Columns)
	diags.Append(columnDiags...)
	if diags.HasError() {
		return diags
	}
	if !columnSignaturesEqual(currentColumns, view.Columns) {
		columns, columnDiags := schemahelpers.ColumnSignaturesValue(ctx, view.Columns)
		diags.Append(columnDiags...)
		if diags.HasError() {
			return diags
		}
		state.Columns = columns
	}

	return diags
}

func columnSignaturesEqual(left []dbops.Column, right []dbops.Column) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Name != right[i].Name ||
			left[i].Nullable != right[i].Nullable ||
			strings.TrimSpace(left[i].Type) != strings.TrimSpace(right[i].Type) {
			return false
		}
	}
	return true
}
