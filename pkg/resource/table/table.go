package table

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/pkg/resource/schemahelpers"
)

//go:embed table.md
var tableResourceDescription string

var (
	_ resource.Resource               = &Resource{}
	_ resource.ResourceWithConfigure  = &Resource{}
	_ resource.ResourceWithModifyPlan = &Resource{}
)

func NewResource() resource.Resource {
	return &Resource{}
}

type Resource struct {
	client dbops.Client
}

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_table"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"cluster_name": schema.StringAttribute{
				Optional:    true,
				Description: "Name of the cluster to create the table into. If omitted, the DDL runs only on the connected replica.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Stable identifier in the form cluster:database.table or database.table",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"qualified_name": schema.StringAttribute{
				Computed:    true,
				Description: "Qualified object name in the form database.table",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"create_statement": schema.StringAttribute{
				Computed:    true,
				Description: "Canonical CREATE statement reported by ClickHouse",
			},
			"database": schema.StringAttribute{
				Required:    true,
				Description: "Database name that owns the table",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Table name",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"engine": schema.StringAttribute{
				Required:    true,
				Description: "Raw ClickHouse engine expression, for example MergeTree(), Distributed(...), or Kafka(...)",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"columns": schemahelpers.ColumnsAttributeWithPlanModifiers("Structured column definitions. This can be assigned directly from a local list of objects.", nil),
			"partition_by": schema.StringAttribute{
				Optional:    true,
				Description: "Raw PARTITION BY clause expression",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"order_by": schema.StringAttribute{
				Optional:    true,
				Description: "Raw ORDER BY clause expression",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"primary_key": schema.StringAttribute{
				Optional:    true,
				Description: "Raw PRIMARY KEY clause expression",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"sample_by": schema.StringAttribute{
				Optional:    true,
				Description: "Raw SAMPLE BY clause expression",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"ttl": schema.StringAttribute{
				Optional:    true,
				Description: "Raw TTL clause expression",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"settings": schema.StringAttribute{
				Optional:    true,
				Description: "Raw SETTINGS clause body",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"as_select": schema.StringAttribute{
				Optional:    true,
				Description: "Optional raw query appended as AS <query> after the table definition",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
		MarkdownDescription: tableResourceDescription,
	}
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	r.client = req.ProviderData.(dbops.Client)
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	if r.client == nil {
		return
	}

	var plan TableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	desiredTable, err := expandTableModel(ctx, plan)
	resp.Diagnostics.Append(schemahelpers.DiagnosticsFromErr("Invalid table configuration", err)...)
	if resp.Diagnostics.HasError() {
		return
	}

	capabilities, err := r.client.GetTableEngineCapabilities(ctx, desiredTable.Engine)
	if err != nil {
		resp.Diagnostics.AddError("Error reading table engine capabilities", fmt.Sprintf("%+v\n", err))
		return
	}

	resp.Diagnostics.Append(validateTableForEngine(desiredTable, capabilities)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if req.State.Raw.IsNull() {
		return
	}

	var state TableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	currentTable, err := expandTableModel(ctx, state)
	resp.Diagnostics.Append(schemahelpers.DiagnosticsFromErr("Invalid table configuration", err)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settingNames := collectSettingNames(currentTable.Settings, desiredTable.Settings)
	settingCapabilities, err := r.client.GetTableSettingCapabilities(ctx, currentTable.Engine, settingNames)
	if err != nil {
		resp.Diagnostics.AddError("Error reading table setting capabilities", fmt.Sprintf("%+v\n", err))
		return
	}

	updatePlan, err := planTableUpdate(currentTable, desiredTable, capabilities, settingCapabilities)
	if err != nil {
		resp.Diagnostics.AddError("Error planning table update", fmt.Sprintf("%+v\n", err))
		return
	}

	for attr := range updatePlan.ReplaceAttrs {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root(attr))
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, diags := r.createTable(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	table, err := r.client.GetTable(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer())
	if err != nil {
		resp.Diagnostics.AddError("Error reading table", fmt.Sprintf("%+v\n", err))
		return
	}

	if table == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	var settingCapabilities map[string]dbops.TableSettingCapability
	if normalizeSQL(table.Settings) != "" {
		var err error
		settingCapabilities, err = r.client.GetTableSettingCapabilities(ctx, table.Engine, collectSettingNames(table.Settings))
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Error reading table setting capabilities",
				fmt.Sprintf("Skipping remote settings reconciliation because setting capabilities could not be read: %+v", err),
			)
		}
	}

	resp.Diagnostics.Append(syncTableState(ctx, &state, table, settingCapabilities)...)
	if resp.Diagnostics.HasError() {
		return
	}

	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, table.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan TableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	currentTable, err := expandTableModel(ctx, state)
	resp.Diagnostics.Append(schemahelpers.DiagnosticsFromErr("Invalid table configuration", err)...)
	if resp.Diagnostics.HasError() {
		return
	}

	desiredTable, err := expandTableModel(ctx, plan)
	resp.Diagnostics.Append(schemahelpers.DiagnosticsFromErr("Invalid table configuration", err)...)
	if resp.Diagnostics.HasError() {
		return
	}

	capabilities, err := r.client.GetTableEngineCapabilities(ctx, desiredTable.Engine)
	if err != nil {
		resp.Diagnostics.AddError("Error reading table engine capabilities", fmt.Sprintf("%+v\n", err))
		return
	}

	resp.Diagnostics.Append(validateTableForEngine(desiredTable, capabilities)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settingNames := collectSettingNames(currentTable.Settings, desiredTable.Settings)
	settingCapabilities, err := r.client.GetTableSettingCapabilities(ctx, currentTable.Engine, settingNames)
	if err != nil {
		resp.Diagnostics.AddError("Error reading table setting capabilities", fmt.Sprintf("%+v\n", err))
		return
	}

	updatePlan, err := planTableUpdate(currentTable, desiredTable, capabilities, settingCapabilities)
	if err != nil {
		resp.Diagnostics.AddError("Error planning table update", fmt.Sprintf("%+v\n", err))
		return
	}
	if len(updatePlan.ReplaceAttrs) > 0 {
		resp.Diagnostics.AddError(
			"Table update requires replacement",
			"The planned table change requires Terraform to replace the resource instead of updating it in place.",
		)
		return
	}

	for _, actionGroup := range updatePlan.ActionGroups {
		if err := r.client.AlterTable(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer(), actionGroup); err != nil {
			resp.Diagnostics.AddError("Error updating table", fmt.Sprintf("%+v\n", err))
			return
		}
	}

	updatedTable, err := r.client.GetTable(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer())
	if err != nil {
		resp.Diagnostics.AddError("Error reading updated table", fmt.Sprintf("%+v\n", err))
		return
	}
	if updatedTable == nil {
		resp.Diagnostics.AddError("Error reading updated table", "Updated table was not found after applying ALTER TABLE statements.")
		return
	}

	newState := plan
	schemahelpers.SyncObjectState(newState.ClusterName, newState.Database, newState.Name, updatedTable.CreateStatement, &newState.ID, &newState.QualifiedName, &newState.CreateStatement)
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteTable(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer()); err != nil {
		resp.Diagnostics.AddError("Error deleting table", fmt.Sprintf("%+v\n", err))
	}
}

func (r *Resource) createTable(ctx context.Context, plan TableResourceModel) (*TableResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	table, err := expandTableModel(ctx, plan)
	diags.Append(schemahelpers.DiagnosticsFromErr("Invalid table configuration", err)...)
	if diags.HasError() {
		return nil, diags
	}

	capabilities, err := r.client.GetTableEngineCapabilities(ctx, table.Engine)
	diags.Append(schemahelpers.DiagnosticsFromErr("Invalid table configuration", err)...)
	if diags.HasError() {
		return nil, diags
	}

	diags.Append(validateTableForEngine(table, capabilities)...)
	if diags.HasError() {
		return nil, diags
	}

	createdTable, err := r.client.CreateTable(ctx, table, plan.ClusterName.ValueStringPointer())
	diags.Append(schemahelpers.DiagnosticsFromErr("Invalid table configuration", err)...)
	if diags.HasError() {
		return nil, diags
	}
	if createdTable == nil {
		diags.AddError("Error creating table", "ClickHouse returned no metadata for the created table.")
		return nil, diags
	}

	state := plan
	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, createdTable.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)

	return &state, diags
}

func expandTableModel(ctx context.Context, plan TableResourceModel) (dbops.Table, error) {
	columns, diags := schemahelpers.ExpandColumns(ctx, plan.Columns)
	if diags.HasError() {
		return dbops.Table{}, schemahelpers.DiagnosticsError(diags)
	}

	return dbops.Table{
		Database:    plan.Database.ValueString(),
		Name:        plan.Name.ValueString(),
		Engine:      plan.Engine.ValueString(),
		Columns:     columns,
		PartitionBy: plan.PartitionBy.ValueString(),
		OrderBy:     plan.OrderBy.ValueString(),
		PrimaryKey:  plan.PrimaryKey.ValueString(),
		SampleBy:    plan.SampleBy.ValueString(),
		TTL:         plan.TTL.ValueString(),
		Settings:    plan.Settings.ValueString(),
		AsSelect:    plan.AsSelect.ValueString(),
	}, nil
}

func syncTableState(ctx context.Context, state *TableResourceModel, table *dbops.Table, settingCapabilities map[string]dbops.TableSettingCapability) diag.Diagnostics {
	var diags diag.Diagnostics

	state.Engine = syncEquivalentString(state.Engine, table.Engine, enginesEquivalent)
	state.PartitionBy = syncEquivalentString(state.PartitionBy, table.PartitionBy, expressionsEqual)
	state.OrderBy = syncEquivalentString(state.OrderBy, table.OrderBy, expressionListsEqual)
	state.PrimaryKey = syncManagedEquivalentString(state.PrimaryKey, table.PrimaryKey, expressionListsEqual)
	state.SampleBy = syncEquivalentString(state.SampleBy, table.SampleBy, expressionsEqual)
	state.TTL = syncEquivalentString(state.TTL, table.TTL, ttlExpressionsEqual)
	state.Settings = syncRemoteSettings(state.Settings, table.Settings, settingCapabilities)
	state.AsSelect = syncEquivalentString(state.AsSelect, table.AsSelect, expressionsEqual)

	currentColumns, columnDiags := schemahelpers.ExpandColumns(ctx, state.Columns)
	diags.Append(columnDiags...)
	if diags.HasError() {
		return diags
	}
	if !columnsEqual(currentColumns, table.Columns) {
		state.Columns, columnDiags = schemahelpers.ColumnsValue(ctx, table.Columns)
		diags.Append(columnDiags...)
	}

	return diags
}

func syncEquivalentString(current types.String, remote string, equal func(string, string) bool) types.String {
	if !current.IsNull() && !current.IsUnknown() && equal(current.ValueString(), remote) {
		return current
	}
	if normalizeSQL(remote) == "" {
		return types.StringNull()
	}
	return types.StringValue(remote)
}

func syncManagedEquivalentString(current types.String, remote string, equal func(string, string) bool) types.String {
	if !current.IsNull() && !current.IsUnknown() && equal(current.ValueString(), remote) {
		return current
	}
	if current.IsNull() || (!current.IsUnknown() && normalizeSQL(current.ValueString()) == "") {
		return current
	}
	if normalizeSQL(remote) == "" {
		return types.StringNull()
	}
	return types.StringValue(remote)
}

func settingsStringsEqual(left string, right string) bool {
	leftParsed, leftErr := parseSettings(left)
	rightParsed, rightErr := parseSettings(right)
	if leftErr == nil && rightErr == nil {
		return settingsEqual(leftParsed, rightParsed)
	}
	return normalizeSQL(left) == normalizeSQL(right)
}

func settingsStringsEqualWithCapabilities(left string, right string, capabilities map[string]dbops.TableSettingCapability) bool {
	leftParsed, leftErr := parseSettings(left)
	rightParsed, rightErr := parseSettings(right)
	if leftErr != nil || rightErr != nil {
		return normalizeSQL(left) == normalizeSQL(right)
	}
	return settingsEqual(filterReadonlySettings(leftParsed, capabilities), filterReadonlySettings(rightParsed, capabilities))
}

func syncRemoteSettings(current types.String, remote string, capabilities map[string]dbops.TableSettingCapability) types.String {
	if !current.IsNull() && !current.IsUnknown() && settingsStringsEqualWithCapabilities(current.ValueString(), remote, capabilities) {
		return current
	}
	if normalizeSQL(remote) == "" {
		return types.StringNull()
	}
	if current.IsNull() || (!current.IsUnknown() && normalizeSQL(current.ValueString()) == "") {
		if settingsAppearToBeDefaults(remote, capabilities) {
			return current
		}
	}
	return types.StringValue(remote)
}

func settingsAppearToBeDefaults(raw string, capabilities map[string]dbops.TableSettingCapability) bool {
	if len(capabilities) == 0 {
		return false
	}

	parsed, err := parseSettings(raw)
	if err != nil {
		return false
	}

	for _, setting := range parsed.ordered {
		capability, ok := capabilities[setting.Name]
		if !ok || !capability.Known || !capability.Readonly {
			return false
		}
	}

	return len(parsed.ordered) > 0
}

func filterReadonlySettings(parsed parsedSettings, capabilities map[string]dbops.TableSettingCapability) parsedSettings {
	if len(parsed.ordered) == 0 {
		return parsed
	}

	filtered := parsedSettings{
		ordered: make([]settingAssignment, 0, len(parsed.ordered)),
		values:  make(map[string]string, len(parsed.values)),
	}

	for _, setting := range parsed.ordered {
		capability, ok := capabilities[setting.Name]
		if ok && capability.Known && capability.Readonly {
			continue
		}
		filtered.ordered = append(filtered.ordered, setting)
		filtered.values[setting.Name] = setting.Value
	}

	return filtered
}

func enginesEquivalent(left string, right string) bool {
	return normalizeEngine(left) == normalizeEngine(right)
}

func normalizeEngine(value string) string {
	value = normalizeSQL(value)
	if strings.HasSuffix(value, "()") {
		return strings.TrimSuffix(value, "()")
	}
	return value
}
