package dbops

import (
	"context"
	"fmt"
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

func (i *impl) deleteIfExists(ctx context.Context, exists bool, dropBuilder querybuilder.QueryBuilder) error {
	if !exists {
		return nil
	}

	sql, err := dropBuilder.Build()
	if err != nil {
		return errors.WithMessage(err, "error building query")
	}

	if err := i.clickhouseClient.Exec(ctx, sql); err != nil {
		return errors.WithMessage(err, "error running query")
	}

	return nil
}

func (i *impl) DeleteTable(ctx context.Context, database string, name string, clusterName *string) error {
	table, err := i.GetTable(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	return i.deleteIfExists(ctx, table != nil, querybuilder.NewDropTable(database, name).WithCluster(clusterName))
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

	definition, err := parseCreateViewDefinition(object.CreateStatement)
	if err != nil {
		return nil, err
	}

	return &View{
		Database:        object.Database,
		Name:            object.Name,
		Columns:         definition.Columns,
		Query:           definition.Query,
		CreateStatement: object.CreateStatement,
	}, nil
}

func (i *impl) DeleteView(ctx context.Context, database string, name string, clusterName *string) error {
	view, err := i.GetView(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	return i.deleteIfExists(ctx, view != nil, querybuilder.NewDropView(database, name).WithCluster(clusterName))
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

	definition, err := parseCreateMaterializedViewDefinition(object.CreateStatement)
	if err != nil {
		return nil, errors.WithMessage(err, "error parsing CREATE MATERIALIZED VIEW statement")
	}

	mv := &MaterializedView{
		Database:        object.Database,
		Name:            object.Name,
		Engine:          definition.Engine,
		ToTable:         definition.ToTable,
		Query:           definition.Query,
		CreateStatement: object.CreateStatement,
	}

	// For engine-backed materialized views, fetch full column details from system.columns
	if definition.ToTable == "" {
		columns, err := i.getTableColumns(ctx, database, name, clusterName)
		if err != nil {
			return nil, errors.WithMessage(err, "error fetching materialized view columns")
		}
		mv.Columns = columns
	} else {
		mv.Columns = definition.Columns
	}

	return mv, nil
}

func (i *impl) DeleteMaterializedView(ctx context.Context, database string, name string, clusterName *string) error {
	view, err := i.GetMaterializedView(ctx, database, name, clusterName)
	if err != nil {
		return err
	}
	return i.deleteIfExists(ctx, view != nil, querybuilder.NewDropMaterializedView(database, name).WithCluster(clusterName))
}

func (i *impl) getSchemaObject(ctx context.Context, database string, name string, clusterName *string, kind schemaObjectKind) (*schemaObject, error) {
	whereConditions := []querybuilder.Where{
		querybuilder.WhereEquals("database", database),
		querybuilder.WhereEquals("name", name),
	}
	switch kind {
	case schemaObjectKindTable:
		whereConditions = append(whereConditions,
			querybuilder.WhereDiffers("engine", "View"),
			querybuilder.WhereDiffers("engine", "MaterializedView"),
		)
	case schemaObjectKindView:
		whereConditions = append(whereConditions, querybuilder.WhereEquals("engine", "View"))
	case schemaObjectKindMaterializedView:
		whereConditions = append(whereConditions, querybuilder.WhereEquals("engine", "MaterializedView"))
	}

	sql, err := querybuilder.NewSelect(
		[]querybuilder.Field{
			querybuilder.NewField("database"),
			querybuilder.NewField("name"),
			querybuilder.NewField("engine"),
			querybuilder.NewField("engine_full"),
			querybuilder.NewField("create_table_query"),
		},
		"system.tables",
	).WithCluster(clusterName).Where(whereConditions...).Build()
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

type createViewDefinition struct {
	Columns []Column
	Query   string
}

type createMaterializedViewDefinition struct {
	Columns []Column
	Engine  string
	ToTable string
	Query   string
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
	seen := make(map[string]struct{})
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

		columnKey := tableColumnKey(column)
		if _, ok := seen[columnKey]; ok {
			return nil
		}
		seen[columnKey] = struct{}{}
		columns = append(columns, column)
		return nil
	})
	if err != nil {
		return nil, errors.WithMessage(err, "error running query")
	}

	return columns, nil
}

func tableColumnKey(column Column) string {
	defaultExpression := ""
	if column.DefaultExpression != nil {
		defaultExpression = *column.DefaultExpression
	}

	materializedExpression := ""
	if column.MaterializedExpression != nil {
		materializedExpression = *column.MaterializedExpression
	}

	aliasExpression := ""
	if column.AliasExpression != nil {
		aliasExpression = *column.AliasExpression
	}

	return strings.Join([]string{
		column.Name,
		column.Type,
		fmt.Sprintf("%t", column.Nullable),
		column.Comment,
		defaultExpression,
		materializedExpression,
		aliasExpression,
	}, "\x00")
}

func parseCreateTableDefinition(createStatement string) (createTableDefinition, error) {
	definition := createTableDefinition{}
	statement := strings.TrimSpace(strings.TrimSuffix(createStatement, ";"))
	if statement == "" {
		return definition, nil
	}

	engineStart, err := findTopLevelKeyword(statement, "ENGINE =", 0)
	if err != nil {
		return definition, err
	}
	if engineStart == -1 {
		return definition, errors.New("unable to locate ENGINE clause in CREATE TABLE statement")
	}

	engineValueStart := engineStart + len("ENGINE =")
	nextClause, err := findNextCreateTableClause(statement, engineValueStart)
	if err != nil {
		return definition, err
	}
	engineValueEnd := len(statement)
	if nextClause != nil {
		engineValueEnd = nextClause.index
	}
	definition.Engine = strings.TrimSpace(statement[engineValueStart:engineValueEnd])

	for clause := nextClause; clause != nil; {
		valueStart := clause.index + len(clause.keyword)
		nextClause, err = findNextCreateTableClause(statement, valueStart)
		if err != nil {
			return definition, err
		}
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

func parseCreateViewDefinition(createStatement string) (createViewDefinition, error) {
	definition := createViewDefinition{}
	statement := strings.TrimSpace(strings.TrimSuffix(createStatement, ";"))
	if statement == "" {
		return definition, nil
	}

	asIndex, err := findTopLevelKeyword(statement, "AS", 0)
	if err != nil {
		return definition, err
	}
	if asIndex == -1 {
		return definition, errors.New("unable to locate AS clause in CREATE VIEW statement")
	}

	definition.Query = strings.TrimSpace(statement[asIndex+len("AS"):])
	prefix := strings.TrimSpace(statement[:asIndex])
	if prefix == "" {
		return definition, nil
	}

	openIndex, closeIndex, ok, err := findTrailingTopLevelParentheses(prefix)
	if err != nil {
		return definition, err
	}
	if !ok {
		return definition, nil
	}

	columns, err := parseColumnSignatures(prefix[openIndex+1 : closeIndex])
	if err != nil {
		return definition, err
	}
	definition.Columns = columns
	return definition, nil
}

func parseCreateMaterializedViewDefinition(createStatement string) (createMaterializedViewDefinition, error) {
	definition := createMaterializedViewDefinition{}
	statement := strings.TrimSpace(strings.TrimSuffix(createStatement, ";"))
	if statement == "" {
		return definition, nil
	}

	// Find AS keyword — the query always follows AS
	asIndex, err := findTopLevelKeyword(statement, "AS", 0)
	if err != nil {
		return definition, err
	}
	if asIndex == -1 {
		return definition, errors.New("unable to locate AS clause in CREATE MATERIALIZED VIEW statement")
	}
	definition.Query = strings.TrimSpace(statement[asIndex+len("AS"):])

	prefix := strings.TrimSpace(statement[:asIndex])

	// Check for TO <table> clause
	toIndex, err := findTopLevelKeyword(prefix, "TO", 0)
	if err != nil {
		return definition, err
	}
	if toIndex != -1 {
		toValue := strings.TrimSpace(prefix[toIndex+len("TO"):])
		// TO table may be followed by column signatures in parens — strip those
		if openIdx, _, ok, err := findTrailingTopLevelParentheses(toValue); err != nil {
			return definition, err
		} else if ok {
			toValue = strings.TrimSpace(toValue[:openIdx])
		}
		definition.ToTable = toValue
		return definition, nil
	}

	// No TO clause — check for ENGINE = clause (engine-backed materialized view)
	engineIndex, err := findTopLevelKeyword(prefix, "ENGINE =", 0)
	if err != nil {
		return definition, err
	}
	if engineIndex != -1 {
		definition.Engine = strings.TrimSpace(prefix[engineIndex+len("ENGINE ="):])
		// Engine value may be followed by ORDER BY etc. — find trailing parens for column signatures
		// above the ENGINE clause
		columnPrefix := strings.TrimSpace(prefix[:engineIndex])
		if openIdx, closeIdx, ok, parseErr := findTrailingTopLevelParentheses(columnPrefix); parseErr != nil {
			return definition, parseErr
		} else if ok {
			columns, parseErr := parseColumnSignatures(columnPrefix[openIdx+1 : closeIdx])
			if parseErr != nil {
				return definition, parseErr
			}
			definition.Columns = columns
		}
		return definition, nil
	}

	// No TO and no ENGINE — check for column signatures in prefix
	if openIdx, closeIdx, ok, parseErr := findTrailingTopLevelParentheses(prefix); parseErr != nil {
		return definition, parseErr
	} else if ok {
		columns, parseErr := parseColumnSignatures(prefix[openIdx+1 : closeIdx])
		if parseErr != nil {
			return definition, parseErr
		}
		definition.Columns = columns
	}

	return definition, nil
}

func findNextCreateTableClause(raw string, start int) (*createTableClause, error) {
	keywords := []string{"PARTITION BY", "ORDER BY", "PRIMARY KEY", "SAMPLE BY", "TTL", "SETTINGS", "AS"}

	var next *createTableClause
	for _, keyword := range keywords {
		index, err := findTopLevelKeyword(raw, keyword, start)
		if err != nil {
			return nil, err
		}
		if index == -1 {
			continue
		}
		if next == nil || index < next.index {
			next = &createTableClause{keyword: keyword, index: index}
		}
	}

	return next, nil
}

func findTrailingTopLevelParentheses(raw string) (int, int, bool, error) {
	state := querybuilder.SQLScanState{}
	closeIndex := -1
	for index := 0; index < len(raw); index++ {
		var err error
		index, err = querybuilder.AdvanceSQLScanState(raw, index, &state)
		if err != nil {
			return 0, 0, false, err
		}
		if !state.IsTopLevel() {
			continue
		}
		if raw[index] == ')' {
			closeIndex = index
		}
	}
	if closeIndex == -1 {
		return 0, 0, false, nil
	}

	state = querybuilder.SQLScanState{}
	openIndex := -1
	for index := 0; index <= closeIndex; index++ {
		ch := raw[index]
		if state.IsTopLevel() && ch == '(' {
			openIndex = index
		}

		var err error
		index, err = querybuilder.AdvanceSQLScanState(raw, index, &state)
		if err != nil {
			return 0, 0, false, err
		}
	}
	if openIndex == -1 || !state.IsTopLevel() {
		return 0, 0, false, errors.New("unable to parse CREATE VIEW column signature")
	}

	if strings.TrimSpace(raw[closeIndex+1:]) != "" {
		return 0, 0, false, nil
	}

	return openIndex, closeIndex, true, nil
}

func parseColumnSignatures(raw string) ([]Column, error) {
	parts, err := splitTopLevelCSV(raw)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, nil
	}

	columns := make([]Column, 0, len(parts))
	for _, part := range parts {
		column, err := parseColumnSignature(part)
		if err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}

	return columns, nil
}

func parseColumnSignature(raw string) (Column, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Column{}, errors.New("empty column signature")
	}

	nameEnd, err := findColumnNameEnd(raw)
	if err != nil {
		return Column{}, err
	}

	name := unquoteIdentifier(strings.TrimSpace(raw[:nameEnd]))
	typeSQL := strings.TrimSpace(raw[nameEnd:])
	if name == "" || typeSQL == "" {
		return Column{}, errors.New("invalid column signature")
	}

	typeWithoutNullable, nullable := unwrapNullableType(typeSQL)
	return Column{
		Name:     name,
		Type:     typeWithoutNullable,
		Nullable: nullable,
	}, nil
}

func findColumnNameEnd(raw string) (int, error) {
	state := querybuilder.SQLScanState{}
	for index := 0; index < len(raw); index++ {
		var err error
		index, err = querybuilder.AdvanceSQLScanState(raw, index, &state)
		if err != nil {
			return 0, err
		}
		if !state.IsTopLevel() {
			continue
		}
		if raw[index] == ' ' || raw[index] == '\t' || raw[index] == '\n' || raw[index] == '\r' {
			return index, nil
		}
	}
	return 0, errors.New("unable to parse column name")
}

func splitTopLevelCSV(raw string) ([]string, error) {
	parts := make([]string, 0)
	start := 0
	state := querybuilder.SQLScanState{}
	for index := 0; index < len(raw); index++ {
		var err error
		index, err = querybuilder.AdvanceSQLScanState(raw, index, &state)
		if err != nil {
			return nil, err
		}
		if !state.IsTopLevel() || raw[index] != ',' {
			continue
		}
		parts = append(parts, strings.TrimSpace(raw[start:index]))
		start = index + 1
	}

	last := strings.TrimSpace(raw[start:])
	if last != "" {
		parts = append(parts, last)
	}

	return parts, nil
}

func unquoteIdentifier(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && raw[0] == '`' && raw[len(raw)-1] == '`' {
		return strings.ReplaceAll(raw[1:len(raw)-1], "``", "`")
	}
	return raw
}

func findTopLevelKeyword(raw string, keyword string, start int) (int, error) {
	state := querybuilder.SQLScanState{}
	for index := 0; index < len(raw); index++ {
		var err error
		index, err = querybuilder.AdvanceSQLScanState(raw, index, &state)
		if err != nil {
			return -1, errors.WithMessage(err, fmt.Sprintf("error scanning SQL near position %d", index))
		}
		if index < start || !state.IsTopLevel() {
			continue
		}
		if !strings.HasPrefix(raw[index:], keyword) {
			continue
		}
		if !isClauseBoundary(raw, index-1) || !isClauseBoundary(raw, index+len(keyword)) {
			continue
		}
		return index, nil
	}

	return -1, nil
}

func isClauseBoundary(raw string, index int) bool {
	if index < 0 || index >= len(raw) {
		return true
	}
	switch raw[index] {
	case ' ', '\t', '\n', '\r', '(':
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
	state := querybuilder.SQLScanState{}
	for index := 0; index < len(inner); index++ {
		var err error
		index, err = querybuilder.AdvanceSQLScanState(inner, index, &state)
		if err != nil {
			return raw, false
		}
	}
	if !state.IsTopLevel() {
		return raw, false
	}

	return strings.TrimSpace(inner), true
}

// ToQueryBuilderColumn converts a single Column to a querybuilder.ColumnDefinition.
func ToQueryBuilderColumn(column Column) querybuilder.ColumnDefinition {
	var comment *string
	if column.Comment != "" {
		comment = &column.Comment
	}

	return querybuilder.ColumnDefinition{
		Name:                   column.Name,
		Type:                   column.Type,
		Nullable:               column.Nullable,
		Comment:                comment,
		DefaultExpression:      column.DefaultExpression,
		MaterializedExpression: column.MaterializedExpression,
		AliasExpression:        column.AliasExpression,
	}
}

func toQueryBuilderColumns(columns []Column) []querybuilder.ColumnDefinition {
	if len(columns) == 0 {
		return nil
	}

	ret := make([]querybuilder.ColumnDefinition, 0, len(columns))
	for _, column := range columns {
		ret = append(ret, ToQueryBuilderColumn(column))
	}

	return ret
}
