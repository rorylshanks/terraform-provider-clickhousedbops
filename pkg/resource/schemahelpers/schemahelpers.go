package schemahelpers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
)

type ColumnModel struct {
	Name                   types.String `tfsdk:"name"`
	Type                   types.String `tfsdk:"type"`
	Nullable               types.Bool   `tfsdk:"nullable"`
	Comment                types.String `tfsdk:"comment"`
	DefaultExpression      types.String `tfsdk:"default_expression"`
	MaterializedExpression types.String `tfsdk:"materialized_expression"`
	AliasExpression        types.String `tfsdk:"alias_expression"`
}

type ColumnSignatureModel struct {
	Name     types.String `tfsdk:"name"`
	Type     types.String `tfsdk:"type"`
	Nullable types.Bool   `tfsdk:"nullable"`
}

func ColumnsAttribute(description string) schema.ListNestedAttribute {
	return ColumnsAttributeWithPlanModifiers(description, []planmodifier.List{
		listplanmodifier.RequiresReplace(),
	})
}

func ColumnSignaturesAttribute(description string) schema.ListNestedAttribute {
	return ColumnSignaturesAttributeWithPlanModifiers(description, []planmodifier.List{
		listplanmodifier.RequiresReplace(),
	})
}

func ColumnsAttributeWithPlanModifiers(description string, modifiers []planmodifier.List) schema.ListNestedAttribute {
	attributes := columnSignatureAttributes()
	attributes["comment"] = schema.StringAttribute{
		Optional:    true,
		Description: "Optional column comment",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
	}
	attributes["default_expression"] = schema.StringAttribute{
		Optional:    true,
		Description: "Raw SQL expression to use in a DEFAULT clause",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
	}
	attributes["materialized_expression"] = schema.StringAttribute{
		Optional:    true,
		Description: "Raw SQL expression to use in a MATERIALIZED clause",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
	}
	attributes["alias_expression"] = schema.StringAttribute{
		Optional:    true,
		Description: "Raw SQL expression to use in an ALIAS clause",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
	}

	return schema.ListNestedAttribute{
		Optional:    true,
		Description: description,
		Validators: []validator.List{
			listvalidator.SizeAtLeast(1),
		},
		PlanModifiers: modifiers,
		NestedObject: schema.NestedAttributeObject{
			Attributes: attributes,
		},
	}
}

func ColumnSignaturesAttributeWithPlanModifiers(description string, modifiers []planmodifier.List) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional:    true,
		Description: description,
		Validators: []validator.List{
			listvalidator.SizeAtLeast(1),
		},
		PlanModifiers: modifiers,
		NestedObject: schema.NestedAttributeObject{
			Attributes: columnSignatureAttributes(),
		},
	}
}

func ExpandColumns(ctx context.Context, columns types.List) ([]dbops.Column, diag.Diagnostics) {
	var diags diag.Diagnostics

	if columns.IsNull() || columns.IsUnknown() {
		return nil, diags
	}

	var models []ColumnModel
	diags.Append(columns.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}

	ret := make([]dbops.Column, 0, len(models))
	for _, model := range models {
		column := dbops.Column{
			Name:     model.Name.ValueString(),
			Type:     model.Type.ValueString(),
			Nullable: model.Nullable.ValueBool(),
		}

		if !model.Comment.IsNull() {
			column.Comment = model.Comment.ValueString()
		}
		if !model.DefaultExpression.IsNull() {
			expr := model.DefaultExpression.ValueString()
			column.DefaultExpression = &expr
		}
		if !model.MaterializedExpression.IsNull() {
			expr := model.MaterializedExpression.ValueString()
			column.MaterializedExpression = &expr
		}
		if !model.AliasExpression.IsNull() {
			expr := model.AliasExpression.ValueString()
			column.AliasExpression = &expr
		}

		ret = append(ret, column)
	}

	return ret, diags
}

func ExpandColumnSignatures(ctx context.Context, columns types.List) ([]dbops.Column, diag.Diagnostics) {
	var diags diag.Diagnostics

	if columns.IsNull() || columns.IsUnknown() {
		return nil, diags
	}

	var models []ColumnSignatureModel
	diags.Append(columns.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}

	ret := make([]dbops.Column, 0, len(models))
	for _, model := range models {
		ret = append(ret, dbops.Column{
			Name:     model.Name.ValueString(),
			Type:     model.Type.ValueString(),
			Nullable: model.Nullable.ValueBool(),
		})
	}

	return ret, diags
}

func ColumnsValue(ctx context.Context, columns []dbops.Column) (types.List, diag.Diagnostics) {
	if len(columns) == 0 {
		return types.ListNull(types.ObjectType{AttrTypes: columnObjectAttrTypes()}), nil
	}

	models := make([]ColumnModel, 0, len(columns))
	for _, column := range columns {
		model := ColumnModel{
			Name:     types.StringValue(column.Name),
			Type:     types.StringValue(column.Type),
			Nullable: types.BoolValue(column.Nullable),
		}

		if strings.TrimSpace(column.Comment) != "" {
			model.Comment = types.StringValue(column.Comment)
		} else {
			model.Comment = types.StringNull()
		}

		model.DefaultExpression = optionalStringValue(column.DefaultExpression)
		model.MaterializedExpression = optionalStringValue(column.MaterializedExpression)
		model.AliasExpression = optionalStringValue(column.AliasExpression)
		models = append(models, model)
	}

	return types.ListValueFrom(ctx, types.ObjectType{AttrTypes: columnObjectAttrTypes()}, models)
}

func QualifiedName(database string, name string) string {
	return fmt.Sprintf("%s.%s", database, name)
}

func ObjectID(clusterName *string, database string, name string) string {
	if clusterName != nil && *clusterName != "" {
		return fmt.Sprintf("%s:%s", *clusterName, QualifiedName(database, name))
	}

	return QualifiedName(database, name)
}

func DiagnosticsError(diags diag.Diagnostics) error {
	if !diags.HasError() {
		return nil
	}

	return errors.New(diags[0].Summary())
}

func DiagnosticsFromErr(summary string, err error) diag.Diagnostics {
	var diags diag.Diagnostics
	if err != nil {
		diags.AddError(summary, err.Error())
	}
	return diags
}

func SyncObjectState(clusterName types.String, database types.String, name types.String, createStatement string, id *types.String, qualifiedName *types.String, createStatementAttr *types.String) {
	*id = types.StringValue(ObjectID(clusterName.ValueStringPointer(), database.ValueString(), name.ValueString()))
	*qualifiedName = types.StringValue(QualifiedName(database.ValueString(), name.ValueString()))

	if strings.TrimSpace(createStatement) != "" {
		*createStatementAttr = types.StringValue(createStatement)
	} else {
		*createStatementAttr = types.StringNull()
	}
}

func columnSignatureAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Required:    true,
			Description: "Column name",
			Validators: []validator.String{
				stringvalidator.LengthAtLeast(1),
			},
		},
		"type": schema.StringAttribute{
			Required:    true,
			Description: "Column type definition without the Nullable wrapper",
			Validators: []validator.String{
				stringvalidator.LengthAtLeast(1),
			},
		},
		"nullable": schema.BoolAttribute{
			Required:    true,
			Description: "Whether the provider should wrap the column type in Nullable(...)",
		},
	}
}

func columnObjectAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name":                    types.StringType,
		"type":                    types.StringType,
		"nullable":                types.BoolType,
		"comment":                 types.StringType,
		"default_expression":      types.StringType,
		"materialized_expression": types.StringType,
		"alias_expression":        types.StringType,
	}
}

func optionalStringValue(value *string) types.String {
	if value == nil || strings.TrimSpace(*value) == "" {
		return types.StringNull()
	}
	return types.StringValue(*value)
}
