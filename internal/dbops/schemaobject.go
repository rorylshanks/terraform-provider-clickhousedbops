package dbops

import (
	"context"
	"strings"

	"github.com/pingcap/errors"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/clickhouseclient"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/querybuilder"
)

type Column struct {
	Name                   string
	Type                   string
	Nullable               bool
	Comment                string
	DefaultExpression      *string
	MaterializedExpression *string
	AliasExpression        *string
}

type Table struct {
	Database        string
	Name            string
	Engine          string
	Columns         []Column
	PartitionBy     string
	OrderBy         string
	PrimaryKey      string
	SampleBy        string
	TTL             string
	Settings        string
	AsSelect        string
	CreateStatement string
}

type View struct {
	Database        string
	Name            string
	Columns         []Column
	Query           string
	CreateStatement string
}

type MaterializedView struct {
	Database        string
	Name            string
	Columns         []Column
	Engine          string
	Populate        bool
	ToTable         string
	ToColumns       []Column
	Query           string
	CreateStatement string
}

type schemaObject struct {
	Database        string
	Name            string
	Engine          string
	EngineFull      string
	CreateStatement string
}

type schemaObjectKind string

const (
	schemaObjectKindTable            schemaObjectKind = "table"
	schemaObjectKindView             schemaObjectKind = "view"
	schemaObjectKindMaterializedView schemaObjectKind = "materialized_view"
)

func (i *impl) CreateTable(ctx context.Context, table Table, clusterName *string) (*Table, error) {
	builder := querybuilder.NewCreateTable(table.Database, table.Name).
		WithCluster(clusterName).
		WithColumns(toQueryBuilderColumns(table.Columns)).
		WithEngine(table.Engine)
	if table.PartitionBy != "" {
		builder = builder.WithPartitionBy(table.PartitionBy)
	}
	if table.OrderBy != "" {
		builder = builder.WithOrderBy(table.OrderBy)
	}
	if table.PrimaryKey != "" {
		builder = builder.WithPrimaryKey(table.PrimaryKey)
	}
	if table.SampleBy != "" {
		builder = builder.WithSampleBy(table.SampleBy)
	}
	if table.TTL != "" {
		builder = builder.WithTTL(table.TTL)
	}
	if table.Settings != "" {
		builder = builder.WithSettings(table.Settings)
	}
	if table.AsSelect != "" {
		builder = builder.WithAsSelect(table.AsSelect)
	}

	sql, err := builder.Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	return i.GetTable(ctx, table.Database, table.Name, clusterName)
}

func (i *impl) GetTable(ctx context.Context, database string, name string, clusterName *string) (*Table, error) {
	object, err := i.getSchemaObject(ctx, database, name, clusterName, schemaObjectKindTable)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, nil
	}

	columns, err := i.getTableColumns(ctx, database, name, clusterName)
	if err != nil {
		return nil, err
	}
	definition, err := parseCreateTableDefinition(object.CreateStatement)
	if err != nil {
		return nil, err
	}

	engine := definition.Engine
	if strings.TrimSpace(engine) == "" {
		engine = object.EngineFull
		if strings.TrimSpace(engine) == "" {
			engine = object.Engine
		}
	}

	return &Table{
		Database:        object.Database,
		Name:            object.Name,
		Engine:          engine,
		Columns:         columns,
		PartitionBy:     definition.PartitionBy,
		OrderBy:         definition.OrderBy,
		PrimaryKey:      definition.PrimaryKey,
		SampleBy:        definition.SampleBy,
		TTL:             definition.TTL,
		Settings:        definition.Settings,
		AsSelect:        definition.AsSelect,
		CreateStatement: object.CreateStatement,
	}, nil
}

func (i *impl) DeleteTable(ctx context.Context, database string, name string, clusterName *string) error {
	table, err := i.GetTable(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	if table == nil {
		return nil
	}

	sql, err := querybuilder.NewDropTable(database, name).WithCluster(clusterName).Build()
	if err != nil {
		return errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return errors.WithMessage(err, "error running query")
	}

	return nil
}

func (i *impl) AlterTable(ctx context.Context, database string, name string, clusterName *string, actions []string) error {
	sql, err := querybuilder.BuildAlterTable(database, name, clusterName, actions)
	if err != nil {
		return errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return errors.WithMessage(err, "error running query")
	}

	return nil
}

func (i *impl) CreateView(ctx context.Context, view View, clusterName *string) (*View, error) {
	builder := querybuilder.NewCreateView(view.Database, view.Name).
		WithCluster(clusterName).
		WithColumns(toQueryBuilderColumns(view.Columns)).
		WithQuery(view.Query)

	sql, err := builder.Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	return i.GetView(ctx, view.Database, view.Name, clusterName)
}

func (i *impl) GetView(ctx context.Context, database string, name string, clusterName *string) (*View, error) {
	object, err := i.getSchemaObject(ctx, database, name, clusterName, schemaObjectKindView)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, nil
	}

	return &View{
		Database:        object.Database,
		Name:            object.Name,
		CreateStatement: object.CreateStatement,
	}, nil
}

func (i *impl) DeleteView(ctx context.Context, database string, name string, clusterName *string) error {
	view, err := i.GetView(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	if view == nil {
		return nil
	}

	sql, err := querybuilder.NewDropView(database, name).WithCluster(clusterName).Build()
	if err != nil {
		return errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return errors.WithMessage(err, "error running query")
	}

	return nil
}

func (i *impl) CreateMaterializedView(ctx context.Context, view MaterializedView, clusterName *string) (*MaterializedView, error) {
	builder := querybuilder.NewCreateMaterializedView(view.Database, view.Name).
		WithCluster(clusterName).
		WithColumns(toQueryBuilderColumns(view.Columns)).
		WithPopulate(view.Populate).
		WithQuery(view.Query)
	if view.Engine != "" {
		builder = builder.WithEngine(view.Engine)
	}
	if view.ToTable != "" {
		builder = builder.WithToTable(view.ToTable)
	}
	if len(view.ToColumns) > 0 {
		builder = builder.WithToColumns(toQueryBuilderColumns(view.ToColumns))
	}

	sql, err := builder.Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	return i.GetMaterializedView(ctx, view.Database, view.Name, clusterName)
}

func (i *impl) GetMaterializedView(ctx context.Context, database string, name string, clusterName *string) (*MaterializedView, error) {
	object, err := i.getSchemaObject(ctx, database, name, clusterName, schemaObjectKindMaterializedView)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, nil
	}

	return &MaterializedView{
		Database:        object.Database,
		Name:            object.Name,
		CreateStatement: object.CreateStatement,
	}, nil
}

func (i *impl) DeleteMaterializedView(ctx context.Context, database string, name string, clusterName *string) error {
	view, err := i.GetMaterializedView(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	if view == nil {
		return nil
	}

	sql, err := querybuilder.NewDropView(database, name).WithCluster(clusterName).Build()
	if err != nil {
		return errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return errors.WithMessage(err, "error running query")
	}

	return nil
}

func (i *impl) getSchemaObject(ctx context.Context, database string, name string, clusterName *string, kind schemaObjectKind) (*schemaObject, error) {
	sql, err := querybuilder.NewSelect(
		[]querybuilder.Field{
			querybuilder.NewField("database"),
			querybuilder.NewField("name"),
			querybuilder.NewField("engine"),
			querybuilder.NewField("engine_full"),
			querybuilder.NewField("create_table_query"),
		},
		"system.tables",
	).WithCluster(clusterName).Where(
		querybuilder.WhereEquals("database", database),
		querybuilder.WhereEquals("name", name),
	).Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	var object *schemaObject

	err = i.clickhouseClient.Select(ctx, sql, func(data clickhouseclient.Row) error {
		if object != nil {
			return nil
		}

		dbName, err := data.GetString("database")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'database' field")
		}
		objectName, err := data.GetString("name")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'name' field")
		}
		engine, err := data.GetString("engine")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'engine' field")
		}
		engineFull, err := data.GetString("engine_full")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'engine_full' field")
		}
		createStatement, err := data.GetString("create_table_query")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'create_table_query' field")
		}

		object = &schemaObject{
			Database:        dbName,
			Name:            objectName,
			Engine:          engine,
			EngineFull:      engineFull,
			CreateStatement: createStatement,
		}

		return nil
	})
	if err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	if object == nil {
		return nil, nil
	}

	switch kind {
	case schemaObjectKindTable:
		if object.Engine == "View" || object.Engine == "MaterializedView" {
			return nil, nil
		}
	case schemaObjectKindView:
		if object.Engine != "View" {
			return nil, nil
		}
	case schemaObjectKindMaterializedView:
		if object.Engine != "MaterializedView" {
			return nil, nil
		}
	default:
		return nil, errors.New("unsupported schema object kind")
	}

	if strings.TrimSpace(object.EngineFull) == "" {
		object.EngineFull = object.Engine
	}

	return object, nil
}

type createTableDefinition struct {
	Engine      string
	PartitionBy string
	OrderBy     string
	PrimaryKey  string
	SampleBy    string
	TTL         string
	Settings    string
	AsSelect    string
}

type createTableClause struct {
	keyword string
	index   int
}

func (i *impl) getTableColumns(ctx context.Context, database string, name string, clusterName *string) ([]Column, error) {
	sql, err := querybuilder.NewSelect(
		[]querybuilder.Field{
			querybuilder.NewField("name"),
			querybuilder.NewField("type"),
			querybuilder.NewField("default_kind"),
			querybuilder.NewField("default_expression"),
			querybuilder.NewField("comment"),
		},
		"system.columns",
	).WithCluster(clusterName).Where(
		querybuilder.WhereEquals("database", database),
		querybuilder.WhereEquals("table", name),
	).OrderBy(querybuilder.NewField("position"), querybuilder.ASC).Build()
	if err != nil {
		return nil, errors.WithMessage(err, "error building query")
	}

	columns := make([]Column, 0)
	err = i.clickhouseClient.Select(ctx, sql, func(data clickhouseclient.Row) error {
		columnName, err := data.GetString("name")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'name' field")
		}
		columnType, err := data.GetString("type")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'type' field")
		}
		defaultKind, err := data.GetString("default_kind")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'default_kind' field")
		}
		defaultExpression, err := data.GetString("default_expression")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'default_expression' field")
		}
		comment, err := data.GetString("comment")
		if err != nil {
			return errors.WithMessage(err, "error scanning query result, missing 'comment' field")
		}

		typeWithoutNullable, nullable := unwrapNullableType(columnType)
		column := Column{
			Name:     columnName,
			Type:     typeWithoutNullable,
			Nullable: nullable,
			Comment:  strings.TrimSpace(comment),
		}

		switch strings.TrimSpace(defaultKind) {
		case "DEFAULT":
			expr := strings.TrimSpace(defaultExpression)
			column.DefaultExpression = &expr
		case "MATERIALIZED":
			expr := strings.TrimSpace(defaultExpression)
			column.MaterializedExpression = &expr
		case "ALIAS":
			expr := strings.TrimSpace(defaultExpression)
			column.AliasExpression = &expr
		}

		columns = append(columns, column)
		return nil
	})
	if err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	return columns, nil
}

func parseCreateTableDefinition(createStatement string) (createTableDefinition, error) {
	definition := createTableDefinition{}
	statement := strings.TrimSpace(strings.TrimSuffix(createStatement, ";"))
	if statement == "" {
		return definition, nil
	}

	engineStart := findTopLevelKeyword(statement, "ENGINE =", 0)
	if engineStart == -1 {
		return definition, errors.New("unable to locate ENGINE clause in CREATE TABLE statement")
	}

	engineValueStart := engineStart + len("ENGINE =")
	nextClause := findNextCreateTableClause(statement, engineValueStart)
	engineValueEnd := len(statement)
	if nextClause != nil {
		engineValueEnd = nextClause.index
	}
	definition.Engine = strings.TrimSpace(statement[engineValueStart:engineValueEnd])

	for clause := nextClause; clause != nil; {
		valueStart := clause.index + len(clause.keyword)
		nextClause = findNextCreateTableClause(statement, valueStart)
		valueEnd := len(statement)
		if nextClause != nil {
			valueEnd = nextClause.index
		}

		value := strings.TrimSpace(statement[valueStart:valueEnd])
		switch clause.keyword {
		case "PARTITION BY":
			definition.PartitionBy = value
		case "ORDER BY":
			definition.OrderBy = value
		case "PRIMARY KEY":
			definition.PrimaryKey = value
		case "SAMPLE BY":
			definition.SampleBy = value
		case "TTL":
			definition.TTL = value
		case "SETTINGS":
			definition.Settings = value
		case "AS":
			definition.AsSelect = value
			return definition, nil
		}

		clause = nextClause
	}

	return definition, nil
}

func findNextCreateTableClause(raw string, start int) *createTableClause {
	keywords := []string{"PARTITION BY", "ORDER BY", "PRIMARY KEY", "SAMPLE BY", "TTL", "SETTINGS", "AS"}

	var next *createTableClause
	for _, keyword := range keywords {
		index := findTopLevelKeyword(raw, keyword, start)
		if index == -1 {
			continue
		}
		if next == nil || index < next.index {
			next = &createTableClause{keyword: keyword, index: index}
		}
	}

	return next
}

func findTopLevelKeyword(raw string, keyword string, start int) int {
	state := createTableScanState{}
	for index := 0; index < len(raw); index++ {
		var err error
		index, err = advanceCreateTableScanState(raw, index, &state)
		if err != nil {
			return -1
		}
		if index < start || !state.isTopLevel() {
			continue
		}
		if !strings.HasPrefix(raw[index:], keyword) {
			continue
		}
		if !isClauseBoundary(raw, index-1) || !isClauseBoundary(raw, index+len(keyword)) {
			continue
		}
		return index
	}

	return -1
}

func isClauseBoundary(raw string, index int) bool {
	if index < 0 || index >= len(raw) {
		return true
	}
	switch raw[index] {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func unwrapNullableType(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "Nullable(") || !strings.HasSuffix(raw, ")") {
		return raw, false
	}

	inner := raw[len("Nullable(") : len(raw)-1]
	state := createTableScanState{parenDepth: 1}
	for index := 0; index < len(raw); index++ {
		var err error
		index, err = advanceCreateTableScanState(raw, index, &state)
		if err != nil {
			return raw, false
		}
		if raw[index] == ')' && state.parenDepth == 0 && index != len(raw)-1 {
			return raw, false
		}
	}

	return strings.TrimSpace(inner), true
}

type createTableScanState struct {
	parenDepth   int
	bracketDepth int
	braceDepth   int
	inQuote      byte
}

func (s createTableScanState) isTopLevel() bool {
	return s.inQuote == 0 && s.parenDepth == 0 && s.bracketDepth == 0 && s.braceDepth == 0
}

func advanceCreateTableScanState(raw string, index int, state *createTableScanState) (int, error) {
	ch := raw[index]
	if state.inQuote != 0 {
		switch state.inQuote {
		case '\'':
			if ch == '\\' {
				if index+1 < len(raw) {
					return index + 1, nil
				}
				return index, errors.New("unterminated escape")
			}
			if ch == '\'' {
				if index+1 < len(raw) && raw[index+1] == '\'' {
					return index + 1, nil
				}
				state.inQuote = 0
			}
		case '"':
			if ch == '\\' {
				if index+1 < len(raw) {
					return index + 1, nil
				}
				return index, errors.New("unterminated escape")
			}
			if ch == '"' {
				state.inQuote = 0
			}
		case '`':
			if ch == '\\' {
				if index+1 < len(raw) {
					return index + 1, nil
				}
				return index, errors.New("unterminated escape")
			}
			if ch == '`' {
				if index+1 < len(raw) && raw[index+1] == '`' {
					return index + 1, nil
				}
				state.inQuote = 0
			}
		}
		return index, nil
	}

	switch ch {
	case '\'', '"', '`':
		state.inQuote = ch
	case '(':
		state.parenDepth++
	case ')':
		state.parenDepth--
	case '[':
		state.bracketDepth++
	case ']':
		state.bracketDepth--
	case '{':
		state.braceDepth++
	case '}':
		state.braceDepth--
	}

	if state.parenDepth < 0 || state.bracketDepth < 0 || state.braceDepth < 0 {
		return index, errors.New("unbalanced delimiters")
	}

	return index, nil
}

func toQueryBuilderColumns(columns []Column) []querybuilder.ColumnDefinition {
	if len(columns) == 0 {
		return nil
	}

	ret := make([]querybuilder.ColumnDefinition, 0, len(columns))
	for _, column := range columns {
		var comment *string
		if column.Comment != "" {
			comment = &column.Comment
		}

		ret = append(ret, querybuilder.ColumnDefinition{
			Name:                   column.Name,
			Type:                   column.Type,
			Nullable:               column.Nullable,
			Comment:                comment,
			DefaultExpression:      column.DefaultExpression,
			MaterializedExpression: column.MaterializedExpression,
			AliasExpression:        column.AliasExpression,
		})
	}

	return ret
}
