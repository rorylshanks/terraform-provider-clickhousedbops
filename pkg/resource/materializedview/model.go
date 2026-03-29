package materializedview

import "github.com/hashicorp/terraform-plugin-framework/types"

type MaterializedViewResourceModel struct {
	ClusterName     types.String `tfsdk:"cluster_name"`
	ID              types.String `tfsdk:"id"`
	QualifiedName   types.String `tfsdk:"qualified_name"`
	CreateStatement types.String `tfsdk:"create_statement"`
	Database        types.String `tfsdk:"database"`
	Name            types.String `tfsdk:"name"`
	Columns         types.List   `tfsdk:"columns"`
	Engine          types.String `tfsdk:"engine"`
	Populate        types.Bool   `tfsdk:"populate"`
	ToTable         types.String `tfsdk:"to_table"`
	ToColumns       types.List   `tfsdk:"to_columns"`
	Query           types.String `tfsdk:"query"`
}
