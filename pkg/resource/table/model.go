package table

import "github.com/hashicorp/terraform-plugin-framework/types"

type TableResourceModel struct {
	ClusterName     types.String `tfsdk:"cluster_name"`
	ID              types.String `tfsdk:"id"`
	QualifiedName   types.String `tfsdk:"qualified_name"`
	CreateStatement types.String `tfsdk:"create_statement"`
	Database        types.String `tfsdk:"database"`
	Name            types.String `tfsdk:"name"`
	Engine          types.String `tfsdk:"engine"`
	Columns         types.List   `tfsdk:"columns"`
	PartitionBy     types.String `tfsdk:"partition_by"`
	OrderBy         types.String `tfsdk:"order_by"`
	PrimaryKey      types.String `tfsdk:"primary_key"`
	SampleBy        types.String `tfsdk:"sample_by"`
	TTL             types.String `tfsdk:"ttl"`
	Settings        types.String `tfsdk:"settings"`
	AsSelect        types.String `tfsdk:"as_select"`
}
