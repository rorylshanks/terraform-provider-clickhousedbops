package dictionary

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/pkg/resource/schemahelpers"
)

//go:embed dictionary.md
var dictionaryResourceDescription string

var (
	_ resource.Resource                = &Resource{}
	_ resource.ResourceWithConfigure   = &Resource{}
	_ resource.ResourceWithImportState = &Resource{}
)

type attributeModel struct {
	Name              types.String `tfsdk:"name"`
	Type              types.String `tfsdk:"type"`
	Nullable          types.Bool   `tfsdk:"nullable"`
	DefaultExpression types.String `tfsdk:"default_expression"`
	Expression        types.String `tfsdk:"expression"`
	Hierarchical      types.Bool   `tfsdk:"hierarchical"`
	Injective         types.Bool   `tfsdk:"injective"`
	IsObjectID        types.Bool   `tfsdk:"is_object_id"`
}

func NewResource() resource.Resource {
	return &Resource{}
}

type Resource struct {
	client dbops.Client
}

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dictionary"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := schemahelpers.CommonSchemaAttributes("dictionary")
	attrs["attributes"] = schema.ListNestedAttribute{
				Required:    true,
				Description: "Dictionary attributes, including key columns referenced by primary_key. This can be assigned directly from a local list of objects.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Attribute name",
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"type": schema.StringAttribute{
							Required:    true,
							Description: "Attribute type definition without the Nullable wrapper",
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"nullable": schema.BoolAttribute{
							Required:    true,
							Description: "Whether the provider should wrap the attribute type in Nullable(...)",
						},
						"default_expression": schema.StringAttribute{
							Optional:    true,
							Description: "Raw SQL expression to use in a DEFAULT clause",
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"expression": schema.StringAttribute{
							Optional:    true,
							Description: "Raw SQL expression to use in an EXPRESSION clause",
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"hierarchical": schema.BoolAttribute{
							Optional:    true,
							Description: "Whether to append the HIERARCHICAL modifier",
						},
						"injective": schema.BoolAttribute{
							Optional:    true,
							Description: "Whether to append the INJECTIVE modifier",
						},
						"is_object_id": schema.BoolAttribute{
							Optional:    true,
							Description: "Whether to append the IS_OBJECT_ID modifier",
						},
					},
				},
	}
	attrs["primary_key"] = schema.ListAttribute{
		Required:    true,
		ElementType: types.StringType,
		Description: "Ordered list of attribute names used in the PRIMARY KEY clause",
		Validators: []validator.List{
			listvalidator.SizeAtLeast(1),
		},
		PlanModifiers: []planmodifier.List{
			listplanmodifier.RequiresReplace(),
		},
	}
	attrs["source"] = schema.StringAttribute{
		Required:    true,
		Description: "Raw SOURCE clause body, for example CLICKHOUSE(HOST 'localhost' PORT tcpPort() USER 'default' PASSWORD 'test' DB 'posthog' TABLE 'teams_source') or NULL()",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	attrs["layout"] = schema.StringAttribute{
		Required:    true,
		Description: "Raw LAYOUT clause body, for example FLAT() or HASHED()",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	attrs["lifetime"] = schema.StringAttribute{
		Required:    true,
		Description: "Raw LIFETIME clause body, for example 0 or MIN 0 MAX 300",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	attrs["settings"] = schema.StringAttribute{
		Optional:    true,
		Description: "Raw SETTINGS clause body",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	attrs["comment"] = schema.StringAttribute{
		Optional:    true,
		Description: "Comment associated with the dictionary",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
	}
	resp.Schema = schema.Schema{
		Attributes:          attrs,
		MarkdownDescription: dictionaryResourceDescription,
	}
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	r.client = req.ProviderData.(dbops.Client)
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DictionaryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, diags := r.createDictionary(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DictionaryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dictionary, err := r.client.GetDictionary(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer())
	if err != nil {
		resp.Diagnostics.AddError("Error reading dictionary", fmt.Sprintf("%+v\n", err))
		return
	}

	if dictionary == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(syncDictionaryState(ctx, &state, dictionary)...)
	if resp.Diagnostics.HasError() {
		return
	}

	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, dictionary.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unexpected in-place dictionary update",
		"Dictionaries are replacement-only resources. Terraform should plan a replacement instead of calling Update.",
	)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DictionaryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteDictionary(ctx, state.Database.ValueString(), state.Name.ValueString(), state.ClusterName.ValueStringPointer()); err != nil {
		resp.Diagnostics.AddError("Error deleting dictionary", fmt.Sprintf("%+v\n", err))
	}
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	schemahelpers.ImportSchemaObjectState(ctx, req, resp)
}

func (r *Resource) createDictionary(ctx context.Context, plan DictionaryResourceModel) (*DictionaryResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	dictionary, err := expandDictionaryModel(ctx, plan)
	diags.Append(schemahelpers.DiagnosticsFromErr("Invalid dictionary configuration", err)...)
	if diags.HasError() {
		return nil, diags
	}

	createdDictionary, err := r.client.CreateDictionary(ctx, dictionary, plan.ClusterName.ValueStringPointer())
	diags.Append(schemahelpers.DiagnosticsFromErr("Error creating dictionary", err)...)
	if diags.HasError() {
		return nil, diags
	}
	if createdDictionary == nil {
		diags.AddError("Error creating dictionary", "ClickHouse returned no metadata for the created dictionary.")
		return nil, diags
	}

	state := plan
	schemahelpers.SyncObjectState(state.ClusterName, state.Database, state.Name, createdDictionary.CreateStatement, &state.ID, &state.QualifiedName, &state.CreateStatement)

	return &state, diags
}

func syncDictionaryState(ctx context.Context, state *DictionaryResourceModel, dict *dbops.Dictionary) diag.Diagnostics {
	var diags diag.Diagnostics

	state.Comment = schemahelpers.SyncOptionalString(state.Comment, dict.Comment)
	state.Source = schemahelpers.SyncOptionalString(state.Source, dict.Source)
	state.Layout = schemahelpers.SyncOptionalString(state.Layout, dict.Layout)
	state.Lifetime = schemahelpers.SyncOptionalString(state.Lifetime, dict.Lifetime)
	state.Settings = schemahelpers.SyncOptionalString(state.Settings, dict.Settings)

	attrModels := make([]attributeModel, 0, len(dict.Attributes))
	for _, attr := range dict.Attributes {
		model := attributeModel{
			Name:         types.StringValue(attr.Name),
			Type:         types.StringValue(attr.Type),
			Nullable:     types.BoolValue(attr.Nullable),
			Hierarchical: types.BoolValue(attr.Hierarchical),
			Injective:    types.BoolValue(attr.Injective),
			IsObjectID:   types.BoolValue(attr.IsObjectID),
		}
		if attr.DefaultExpression != nil {
			model.DefaultExpression = types.StringValue(*attr.DefaultExpression)
		} else {
			model.DefaultExpression = types.StringNull()
		}
		if attr.Expression != nil {
			model.Expression = types.StringValue(*attr.Expression)
		} else {
			model.Expression = types.StringNull()
		}
		attrModels = append(attrModels, model)
	}

	attrList, attrDiags := types.ListValueFrom(ctx, dictionaryAttributeObjectType(), attrModels)
	diags.Append(attrDiags...)
	if diags.HasError() {
		return diags
	}
	state.Attributes = attrList

	pkList, pkDiags := types.ListValueFrom(ctx, types.StringType, dict.PrimaryKey)
	diags.Append(pkDiags...)
	if diags.HasError() {
		return diags
	}
	state.PrimaryKey = pkList

	return diags
}

func dictionaryAttributeObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"name":               types.StringType,
			"type":               types.StringType,
			"nullable":           types.BoolType,
			"default_expression": types.StringType,
			"expression":         types.StringType,
			"hierarchical":       types.BoolType,
			"injective":          types.BoolType,
			"is_object_id":       types.BoolType,
		},
	}
}

func expandDictionaryModel(ctx context.Context, plan DictionaryResourceModel) (dbops.Dictionary, error) {
	if plan.Attributes.IsNull() || plan.Attributes.IsUnknown() {
		return dbops.Dictionary{}, errors.New("attributes cannot be null")
	}

	var attributeModels []attributeModel
	diags := plan.Attributes.ElementsAs(ctx, &attributeModels, false)
	if diags.HasError() {
		return dbops.Dictionary{}, schemahelpers.DiagnosticsError(diags)
	}

	attributes := make([]dbops.DictionaryAttribute, 0, len(attributeModels))
	attributeNames := make(map[string]struct{}, len(attributeModels))
	for _, model := range attributeModels {
		name := model.Name.ValueString()
		if _, exists := attributeNames[name]; exists {
			return dbops.Dictionary{}, fmt.Errorf("duplicate dictionary attribute %q", name)
		}
		attributeNames[name] = struct{}{}

		attribute := dbops.DictionaryAttribute{
			Name:         name,
			Type:         model.Type.ValueString(),
			Nullable:     model.Nullable.ValueBool(),
			Hierarchical: model.Hierarchical.ValueBool(),
			Injective:    model.Injective.ValueBool(),
			IsObjectID:   model.IsObjectID.ValueBool(),
		}

		if !model.DefaultExpression.IsNull() {
			expr := model.DefaultExpression.ValueString()
			attribute.DefaultExpression = &expr
		}
		if !model.Expression.IsNull() {
			expr := model.Expression.ValueString()
			attribute.Expression = &expr
		}

		attributes = append(attributes, attribute)
	}

	var primaryKey []string
	diags = plan.PrimaryKey.ElementsAs(ctx, &primaryKey, false)
	if diags.HasError() {
		return dbops.Dictionary{}, schemahelpers.DiagnosticsError(diags)
	}
	for _, primaryKeyName := range primaryKey {
		if _, exists := attributeNames[primaryKeyName]; !exists {
			return dbops.Dictionary{}, fmt.Errorf("primary key attribute %q is not defined in attributes", primaryKeyName)
		}
	}

	return dbops.Dictionary{
		Database:   plan.Database.ValueString(),
		Name:       plan.Name.ValueString(),
		Attributes: attributes,
		PrimaryKey: primaryKey,
		Source:     plan.Source.ValueString(),
		Layout:     plan.Layout.ValueString(),
		Lifetime:   plan.Lifetime.ValueString(),
		Settings:   plan.Settings.ValueString(),
		Comment:    plan.Comment.ValueString(),
	}, nil
}
