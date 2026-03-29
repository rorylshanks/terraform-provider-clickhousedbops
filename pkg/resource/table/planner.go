package table

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/dbops"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/querybuilder"
	"github.com/ClickHouse/terraform-provider-clickhousedbops/internal/tableengine"
)

type plannedTableUpdate struct {
	ReplaceAttrs map[string]struct{}
	ActionGroups [][]string
}

type engineUpdateStrategy struct {
	family             tableengine.Family
	allowColumnAlter   bool
	allowSettingsAlter bool
	allowSampleByAlter bool
	allowTTLAlter      bool
	allowOrderByAlter  bool
}

type columnUpdatePlan struct {
	replaceRequired bool
	renameActions   []string
	addActions      []string
	modifyActions   []string
	dropActions     []string
	addedColumns    map[string]dbops.Column
}

type parsedSettings struct {
	ordered []settingAssignment
	values  map[string]string
}

type settingAssignment struct {
	Name  string
	Value string
}

type columnExpression struct {
	kind querybuilder.ColumnExpressionKind
	sql  string
}

func validateTableForEngine(table dbops.Table, capabilities dbops.TableEngineCapabilities) diag.Diagnostics {
	var diags diag.Diagnostics

	family := tableengine.FamilyForName(capabilities.Name)

	if capabilities.Known {
		if family == tableengine.FamilyMergeTree && normalizeSQL(table.OrderBy) == "" {
			diags.AddError(
				"Missing ORDER BY clause",
				fmt.Sprintf("The %q engine family requires ORDER BY. Use at least tuple() when no sort key is needed.", capabilities.Name),
			)
		}
		if !capabilities.SupportsSettings && normalizeSQL(table.Settings) != "" {
			diags.AddError(
				"Unsupported table settings",
				fmt.Sprintf("The %q engine does not support a table-level SETTINGS clause.", capabilities.Name),
			)
		}
		if !capabilities.SupportsSortOrder {
			if normalizeSQL(table.PartitionBy) != "" {
				diags.AddError("Unsupported PARTITION BY clause", fmt.Sprintf("The %q engine does not support PARTITION BY.", capabilities.Name))
			}
			if normalizeSQL(table.OrderBy) != "" {
				diags.AddError("Unsupported ORDER BY clause", fmt.Sprintf("The %q engine does not support ORDER BY.", capabilities.Name))
			}
			if normalizeSQL(table.PrimaryKey) != "" {
				diags.AddError("Unsupported PRIMARY KEY clause", fmt.Sprintf("The %q engine does not support PRIMARY KEY.", capabilities.Name))
			}
			if normalizeSQL(table.SampleBy) != "" {
				diags.AddError("Unsupported SAMPLE BY clause", fmt.Sprintf("The %q engine does not support SAMPLE BY.", capabilities.Name))
			}
		}
		if !capabilities.SupportsTTL && normalizeSQL(table.TTL) != "" {
			diags.AddError("Unsupported TTL clause", fmt.Sprintf("The %q engine does not support TTL.", capabilities.Name))
		}
	}

	if family == tableengine.FamilyKafka {
		insertableColumns := 0
		for _, column := range table.Columns {
			if column.DefaultExpression != nil {
				diags.AddError(
					"Unsupported Kafka column default",
					fmt.Sprintf("Kafka tables do not support DEFAULT column expressions. Column %q defines one.", column.Name),
				)
			}
			if column.MaterializedExpression != nil {
				diags.AddError(
					"Unsupported Kafka materialized column",
					fmt.Sprintf("Kafka tables do not support MATERIALIZED column expressions. Column %q defines one.", column.Name),
				)
			}
			if column.AliasExpression == nil {
				insertableColumns++
			}
		}
		if len(table.Columns) > 0 && insertableColumns == 0 {
			diags.AddError(
				"Invalid Kafka column layout",
				"Kafka tables must expose at least one insertable column. A schema made entirely of ALIAS columns cannot be created.",
			)
		}
	}

	return diags
}

func planTableUpdate(current dbops.Table, desired dbops.Table, capabilities dbops.TableEngineCapabilities, settingCapabilities map[string]dbops.TableSettingCapability) (plannedTableUpdate, error) {
	plan := plannedTableUpdate{
		ReplaceAttrs: make(map[string]struct{}),
	}

	if !expressionsEqual(current.PartitionBy, desired.PartitionBy) {
		plan.ReplaceAttrs["partition_by"] = struct{}{}
	}
	if !expressionListsEqual(current.PrimaryKey, desired.PrimaryKey) {
		plan.ReplaceAttrs["primary_key"] = struct{}{}
	}

	strategy := buildEngineUpdateStrategy(capabilities)

	columnPlan, err := planColumnUpdate(current, desired, strategy)
	if err != nil {
		return plannedTableUpdate{}, err
	}
	if columnPlan.replaceRequired {
		plan.ReplaceAttrs["columns"] = struct{}{}
	}

	if orderChanged := !expressionListsEqual(current.OrderBy, desired.OrderBy); orderChanged {
		if !strategy.allowOrderByAlter || columnPlan.replaceRequired {
			plan.ReplaceAttrs["order_by"] = struct{}{}
		} else {
			action, ok, actionErr := buildOrderByAlterAction(current.OrderBy, desired.OrderBy, columnPlan.addedColumns)
			if actionErr != nil {
				return plannedTableUpdate{}, actionErr
			}
			if !ok {
				plan.ReplaceAttrs["order_by"] = struct{}{}
			} else {
				columnPlan.addActions = append(columnPlan.addActions, action)
			}
		}
	}

	tableActions := make([]string, 0)

	if !expressionsEqual(current.SampleBy, desired.SampleBy) {
		if !strategy.allowSampleByAlter {
			plan.ReplaceAttrs["sample_by"] = struct{}{}
		} else if normalizeSQL(desired.SampleBy) == "" {
			tableActions = append(tableActions, querybuilder.BuildRemoveSampleByAction())
		} else {
			action, actionErr := querybuilder.BuildModifySampleByAction(desired.SampleBy)
			if actionErr != nil {
				return plannedTableUpdate{}, actionErr
			}
			tableActions = append(tableActions, action)
		}
	}

	if !ttlExpressionsEqual(current.TTL, desired.TTL) {
		if !strategy.allowTTLAlter {
			plan.ReplaceAttrs["ttl"] = struct{}{}
		} else if normalizeSQL(desired.TTL) == "" {
			tableActions = append(tableActions, querybuilder.BuildRemoveTTLAction())
		} else {
			action, actionErr := querybuilder.BuildModifyTTLAction(desired.TTL)
			if actionErr != nil {
				return plannedTableUpdate{}, actionErr
			}
			tableActions = append(tableActions, action)
		}
	}

	settingsActions, settingsReplace, err := planSettingsUpdate(current.Settings, desired.Settings, strategy, settingCapabilities)
	if err != nil {
		return plannedTableUpdate{}, err
	}
	if settingsReplace {
		plan.ReplaceAttrs["settings"] = struct{}{}
	}

	if len(plan.ReplaceAttrs) > 0 {
		return plan, nil
	}

	if len(columnPlan.renameActions) > 0 {
		plan.ActionGroups = append(plan.ActionGroups, columnPlan.renameActions)
	}
	if len(columnPlan.addActions) > 0 {
		plan.ActionGroups = append(plan.ActionGroups, columnPlan.addActions)
	}
	if len(columnPlan.modifyActions) > 0 {
		plan.ActionGroups = append(plan.ActionGroups, columnPlan.modifyActions)
	}
	if len(tableActions) > 0 {
		plan.ActionGroups = append(plan.ActionGroups, tableActions)
	}
	if len(settingsActions) > 0 {
		plan.ActionGroups = append(plan.ActionGroups, settingsActions)
	}
	if len(columnPlan.dropActions) > 0 {
		plan.ActionGroups = append(plan.ActionGroups, columnPlan.dropActions)
	}

	return plan, nil
}

func buildEngineUpdateStrategy(capabilities dbops.TableEngineCapabilities) engineUpdateStrategy {
	family := tableengine.FamilyForName(capabilities.Name)
	switch family {
	case tableengine.FamilyMergeTree:
		return engineUpdateStrategy{
			family:             family,
			allowColumnAlter:   true,
			allowSettingsAlter: true,
			allowSampleByAlter: true,
			allowTTLAlter:      true,
			allowOrderByAlter:  true,
		}
	case tableengine.FamilyDistributed:
		return engineUpdateStrategy{
			family:           family,
			allowColumnAlter: true,
		}
	default:
		return engineUpdateStrategy{family: family}
	}
}

func planColumnUpdate(current dbops.Table, desired dbops.Table, strategy engineUpdateStrategy) (columnUpdatePlan, error) {
	result := columnUpdatePlan{
		addedColumns: make(map[string]dbops.Column),
	}

	if columnsEqual(current.Columns, desired.Columns) {
		return result, nil
	}
	if !strategy.allowColumnAlter {
		result.replaceRequired = true
		return result, nil
	}

	currentColumns, renameActions, renameErr := applySafeRenames(current, desired)
	if renameErr != nil {
		return columnUpdatePlan{}, renameErr
	}
	result.renameActions = renameActions

	if hasDuplicateColumnNames(currentColumns) || hasDuplicateColumnNames(desired.Columns) {
		result.replaceRequired = true
		return result, nil
	}

	currentMap := make(map[string]dbops.Column, len(currentColumns))
	currentOrder := make([]string, 0, len(currentColumns))
	for _, column := range currentColumns {
		currentMap[column.Name] = column
		currentOrder = append(currentOrder, column.Name)
	}

	desiredNames := make(map[string]struct{}, len(desired.Columns))
	for _, column := range desired.Columns {
		desiredNames[column.Name] = struct{}{}
	}

	refs := collectIdentifierReferences(current, desired)
	for _, column := range currentColumns {
		if _, ok := desiredNames[column.Name]; ok {
			continue
		}
		if refs[column.Name] {
			result.replaceRequired = true
			return result, nil
		}
	}

	for index, desiredColumn := range desired.Columns {
		previousName := ""
		if index > 0 {
			previousName = desired.Columns[index-1].Name
		}

		currentColumn, exists := currentMap[desiredColumn.Name]
		if !exists {
			position := columnPositionFor(previousName)
			action, err := querybuilder.BuildAddColumnAction(toQueryBuilderColumn(desiredColumn), position)
			if err != nil {
				return columnUpdatePlan{}, err
			}
			result.addActions = append(result.addActions, action)
			result.addedColumns[desiredColumn.Name] = desiredColumn
			currentOrder = insertNameAfter(currentOrder, desiredColumn.Name, previousName)
			currentMap[desiredColumn.Name] = desiredColumn
			continue
		}

		positionChanged := !columnInDesiredPosition(currentOrder, desiredColumn.Name, previousName)
		commentChanged := normalizeSQL(currentColumn.Comment) != normalizeSQL(desiredColumn.Comment)
		typeChanged := normalizeSQL(currentColumn.Type) != normalizeSQL(desiredColumn.Type) || currentColumn.Nullable != desiredColumn.Nullable
		oldExpression := extractColumnExpression(currentColumn)
		newExpression := extractColumnExpression(desiredColumn)
		expressionChanged := oldExpression.kind != newExpression.kind || normalizeSQL(oldExpression.sql) != normalizeSQL(newExpression.sql)

		if oldExpression.kind != "" && (oldExpression.kind != newExpression.kind || normalizeSQL(newExpression.sql) == "") {
			action, err := querybuilder.BuildRemoveColumnExpressionAction(desiredColumn.Name, oldExpression.kind)
			if err != nil {
				return columnUpdatePlan{}, err
			}
			result.modifyActions = append(result.modifyActions, action)
		}

		if typeChanged || positionChanged || (normalizeSQL(newExpression.sql) != "" && expressionChanged) {
			position := (*querybuilder.ColumnPosition)(nil)
			if positionChanged {
				position = columnPositionFor(previousName)
			}
			action, err := querybuilder.BuildModifyColumnAction(toQueryBuilderColumn(desiredColumn), position)
			if err != nil {
				return columnUpdatePlan{}, err
			}
			result.modifyActions = append(result.modifyActions, action)
		}

		if commentChanged {
			var (
				action string
				err    error
			)
			if normalizeSQL(desiredColumn.Comment) == "" {
				action, err = querybuilder.BuildRemoveColumnCommentAction(desiredColumn.Name)
			} else {
				action, err = querybuilder.BuildCommentColumnAction(desiredColumn.Name, desiredColumn.Comment)
			}
			if err != nil {
				return columnUpdatePlan{}, err
			}
			result.modifyActions = append(result.modifyActions, action)
		}

		if positionChanged {
			currentOrder = moveNameAfter(currentOrder, desiredColumn.Name, previousName)
		}
		currentMap[desiredColumn.Name] = desiredColumn
	}

	for _, column := range currentColumns {
		if _, ok := desiredNames[column.Name]; ok {
			continue
		}
		action, err := querybuilder.BuildDropColumnAction(column.Name)
		if err != nil {
			return columnUpdatePlan{}, err
		}
		result.dropActions = append(result.dropActions, action)
	}

	return result, nil
}

func applySafeRenames(current dbops.Table, desired dbops.Table) ([]dbops.Column, []string, error) {
	renamed := make([]dbops.Column, len(current.Columns))
	copy(renamed, current.Columns)

	currentNames := make(map[string]struct{}, len(current.Columns))
	desiredNames := make(map[string]struct{}, len(desired.Columns))
	for _, column := range current.Columns {
		currentNames[column.Name] = struct{}{}
	}
	for _, column := range desired.Columns {
		desiredNames[column.Name] = struct{}{}
	}

	refs := collectIdentifierReferences(current, desired)
	actions := make([]string, 0)

	for idx := 0; idx < len(renamed) && idx < len(desired.Columns); idx++ {
		currentColumn := renamed[idx]
		desiredColumn := desired.Columns[idx]
		if currentColumn.Name == desiredColumn.Name {
			continue
		}
		if _, exists := desiredNames[currentColumn.Name]; exists {
			continue
		}
		if _, exists := currentNames[desiredColumn.Name]; exists {
			continue
		}
		if !columnsEqualIgnoringName(currentColumn, desiredColumn) {
			continue
		}
		if refs[currentColumn.Name] || refs[desiredColumn.Name] {
			continue
		}

		action, err := querybuilder.BuildRenameColumnAction(currentColumn.Name, desiredColumn.Name)
		if err != nil {
			return nil, nil, err
		}
		actions = append(actions, action)
		renamed[idx].Name = desiredColumn.Name
		delete(currentNames, currentColumn.Name)
		currentNames[desiredColumn.Name] = struct{}{}
	}

	return renamed, actions, nil
}

func buildOrderByAlterAction(current string, desired string, addedColumns map[string]dbops.Column) (string, bool, error) {
	currentExprs, err := splitExpressionList(current)
	if err != nil {
		return "", false, nil
	}
	desiredExprs, err := splitExpressionList(desired)
	if err != nil {
		return "", false, nil
	}
	if len(desiredExprs) <= len(currentExprs) || len(currentExprs) == 0 {
		return "", false, nil
	}
	for idx, currentExpr := range currentExprs {
		if normalizeSQL(currentExpr) != normalizeSQL(desiredExprs[idx]) {
			return "", false, nil
		}
	}

	for _, expr := range desiredExprs[len(currentExprs):] {
		columnName, ok := simpleIdentifier(expr)
		if !ok {
			return "", false, nil
		}
		column, ok := addedColumns[columnName]
		if !ok {
			return "", false, nil
		}
		if column.DefaultExpression != nil || column.MaterializedExpression != nil || column.AliasExpression != nil {
			return "", false, nil
		}
	}

	action, err := querybuilder.BuildModifyOrderByAction(desired)
	if err != nil {
		return "", false, err
	}

	return action, true, nil
}

func planSettingsUpdate(current string, desired string, strategy engineUpdateStrategy, capabilities map[string]dbops.TableSettingCapability) ([]string, bool, error) {
	current = normalizeSQL(current)
	desired = normalizeSQL(desired)
	if current == desired {
		return nil, false, nil
	}

	currentParsed, err := parseSettings(current)
	if err != nil {
		return nil, false, fmt.Errorf("unable to parse current table settings: %w", err)
	}
	desiredParsed, err := parseSettings(desired)
	if err != nil {
		return nil, false, fmt.Errorf("unable to parse desired table settings: %w", err)
	}
	if settingsEqual(currentParsed, desiredParsed) {
		return nil, false, nil
	}
	if !strategy.allowSettingsAlter {
		return nil, true, nil
	}

	actions := make([]string, 0)
	seen := make(map[string]struct{})

	for _, setting := range desiredParsed.ordered {
		seen[setting.Name] = struct{}{}
		if currentValue, ok := currentParsed.values[setting.Name]; ok && normalizeSQL(currentValue) == normalizeSQL(setting.Value) {
			continue
		}
		capability, ok := capabilities[setting.Name]
		if !ok || !capability.Known || capability.Readonly {
			return nil, true, nil
		}
		action, err := querybuilder.BuildModifySettingAction(setting.Name, setting.Value)
		if err != nil {
			return nil, false, err
		}
		actions = append(actions, action)
	}

	for _, setting := range currentParsed.ordered {
		if _, ok := seen[setting.Name]; ok {
			continue
		}
		capability, ok := capabilities[setting.Name]
		if !ok || !capability.Known || capability.Readonly {
			return nil, true, nil
		}
		action, err := querybuilder.BuildResetSettingAction(setting.Name)
		if err != nil {
			return nil, false, err
		}
		actions = append(actions, action)
	}

	return actions, false, nil
}

func collectSettingNames(values ...string) []string {
	unique := make(map[string]struct{})
	for _, raw := range values {
		parsed, err := parseSettings(raw)
		if err != nil {
			continue
		}
		for name := range parsed.values {
			unique[name] = struct{}{}
		}
	}

	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	return result
}

func parseSettings(raw string) (parsedSettings, error) {
	settings := parsedSettings{
		values: make(map[string]string),
	}
	raw = normalizeSQL(raw)
	if raw == "" {
		return settings, nil
	}

	parts, err := splitTopLevel(raw, ',')
	if err != nil {
		return parsedSettings{}, err
	}

	for _, part := range parts {
		part = normalizeSQL(part)
		if part == "" {
			continue
		}
		name, value, found, splitErr := splitTopLevelPair(part, '=')
		if splitErr != nil {
			return parsedSettings{}, splitErr
		}
		if !found || normalizeSQL(name) == "" || normalizeSQL(value) == "" {
			return parsedSettings{}, fmt.Errorf("unable to parse table setting %q", part)
		}
		assignment := settingAssignment{
			Name:  normalizeIdentifier(name),
			Value: normalizeSQL(value),
		}
		settings.ordered = append(settings.ordered, assignment)
		settings.values[assignment.Name] = assignment.Value
	}

	return settings, nil
}

func settingsEqual(left parsedSettings, right parsedSettings) bool {
	if len(left.values) != len(right.values) {
		return false
	}
	for name, value := range left.values {
		if normalizeSQL(right.values[name]) != normalizeSQL(value) {
			return false
		}
	}
	return true
}

func expressionsEqual(left string, right string) bool {
	return normalizeSQL(left) == normalizeSQL(right)
}

func expressionListsEqual(left string, right string) bool {
	leftParts, leftErr := splitExpressionList(left)
	rightParts, rightErr := splitExpressionList(right)
	if leftErr != nil || rightErr != nil {
		return normalizeSQL(left) == normalizeSQL(right)
	}
	if len(leftParts) != len(rightParts) {
		return false
	}
	for index := range leftParts {
		if normalizeSQL(leftParts[index]) != normalizeSQL(rightParts[index]) {
			return false
		}
	}
	return true
}

func ttlExpressionsEqual(left string, right string) bool {
	return normalizeTTLExpression(left) == normalizeTTLExpression(right)
}

func normalizeTTLExpression(value string) string {
	value = normalizeSQL(value)
	if value == "" {
		return ""
	}

	replacer := ttlIntervalFuncPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := ttlIntervalFuncPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		return fmt.Sprintf("INTERVAL(%s,%s)", normalizeSQL(parts[2]), strings.ToUpper(parts[1]))
	})

	return ttlIntervalKeywordPattern.ReplaceAllStringFunc(replacer, func(match string) string {
		parts := ttlIntervalKeywordPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		return fmt.Sprintf("INTERVAL(%s,%s)", normalizeSQL(parts[1]), strings.ToUpper(parts[2]))
	})
}

func splitExpressionList(raw string) ([]string, error) {
	raw = normalizeSQL(raw)
	if raw == "" {
		return nil, nil
	}
	raw = unwrapOuterParens(raw)
	parts, err := splitTopLevel(raw, ',')
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = normalizeSQL(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result, nil
}

func splitTopLevel(raw string, separator rune) ([]string, error) {
	if normalizeSQL(raw) == "" {
		return nil, nil
	}

	var (
		parts []string
		start int
		state sqlScanState
	)

	for index := 0; index < len(raw); index++ {
		ch := raw[index]
		var err error
		index, err = advanceSQLScanState(raw, index, &state)
		if err != nil {
			return nil, fmt.Errorf("unbalanced SQL fragment %q", raw)
		}

		if state.isTopLevel() && rune(ch) == separator {
			parts = append(parts, raw[start:index])
			start = index + 1
		}
	}

	if !state.isBalanced() {
		return nil, fmt.Errorf("unbalanced SQL fragment %q", raw)
	}

	parts = append(parts, raw[start:])
	return parts, nil
}

func splitTopLevelPair(raw string, separator rune) (string, string, bool, error) {
	parts, err := splitTopLevel(raw, separator)
	if err != nil {
		return "", "", false, err
	}
	if len(parts) < 2 {
		return "", "", false, nil
	}

	left := parts[0]
	right := strings.Join(parts[1:], string(separator))
	return left, right, true, nil
}

func columnPositionFor(previousName string) *querybuilder.ColumnPosition {
	if previousName == "" {
		return querybuilder.FirstColumnPosition()
	}
	return querybuilder.AfterColumnPosition(previousName)
}

func toQueryBuilderColumn(column dbops.Column) querybuilder.ColumnDefinition {
	var comment *string
	if normalizeSQL(column.Comment) != "" {
		value := column.Comment
		comment = &value
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

func extractColumnExpression(column dbops.Column) columnExpression {
	switch {
	case column.DefaultExpression != nil:
		return columnExpression{kind: querybuilder.ColumnExpressionKindDefault, sql: *column.DefaultExpression}
	case column.MaterializedExpression != nil:
		return columnExpression{kind: querybuilder.ColumnExpressionKindMaterialized, sql: *column.MaterializedExpression}
	case column.AliasExpression != nil:
		return columnExpression{kind: querybuilder.ColumnExpressionKindAlias, sql: *column.AliasExpression}
	default:
		return columnExpression{}
	}
}

func collectIdentifierReferences(current dbops.Table, desired dbops.Table) map[string]bool {
	referenced := make(map[string]bool)
	for _, expression := range collectExpressions(current, desired) {
		for _, name := range extractIdentifiers(expression) {
			referenced[name] = true
		}
	}
	return referenced
}

func collectExpressions(tables ...dbops.Table) []string {
	expressions := make([]string, 0)
	for _, table := range tables {
		if value := normalizeSQL(table.PartitionBy); value != "" {
			expressions = append(expressions, value)
		}
		if value := normalizeSQL(table.OrderBy); value != "" {
			expressions = append(expressions, value)
		}
		if value := normalizeSQL(table.PrimaryKey); value != "" {
			expressions = append(expressions, value)
		}
		if value := normalizeSQL(table.SampleBy); value != "" {
			expressions = append(expressions, value)
		}
		if value := normalizeSQL(table.TTL); value != "" {
			expressions = append(expressions, value)
		}
		for _, column := range table.Columns {
			if column.DefaultExpression != nil {
				expressions = append(expressions, normalizeSQL(*column.DefaultExpression))
			}
			if column.MaterializedExpression != nil {
				expressions = append(expressions, normalizeSQL(*column.MaterializedExpression))
			}
			if column.AliasExpression != nil {
				expressions = append(expressions, normalizeSQL(*column.AliasExpression))
			}
		}
	}
	return expressions
}

var (
	identifierPattern         = regexp.MustCompile("`[^`]+`|[A-Za-z_][A-Za-z0-9_]*")
	ttlIntervalFuncPattern    = regexp.MustCompile(`(?i)\btoInterval([A-Za-z]+)\s*\(\s*([^)]+?)\s*\)`)
	ttlIntervalKeywordPattern = regexp.MustCompile(`(?i)\bINTERVAL\s+([^\s,()]+)\s+([A-Za-z]+)`)
)

func extractIdentifiers(expression string) []string {
	matches := identifierPattern.FindAllString(expression, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, normalizeIdentifier(match))
	}
	return result
}

func simpleIdentifier(expression string) (string, bool) {
	expression = normalizeSQL(expression)
	if expression == "" {
		return "", false
	}
	unquoted := normalizeIdentifier(expression)
	if unquoted == "" {
		return "", false
	}
	if !identifierPattern.MatchString(unquoted) || identifierPattern.FindString(unquoted) != unquoted {
		return "", false
	}
	return unquoted, true
}

func unwrapOuterParens(value string) string {
	value = normalizeSQL(value)
	if len(value) < 2 || value[0] != '(' || value[len(value)-1] != ')' {
		return value
	}

	state := sqlScanState{}
	for index := 0; index < len(value); index++ {
		ch := value[index]
		var err error
		index, err = advanceSQLScanState(value, index, &state)
		if err != nil {
			return value
		}
		if ch == ')' && state.parenDepth == 0 && index != len(value)-1 {
			return value
		}
	}

	return normalizeSQL(value[1 : len(value)-1])
}

type sqlScanState struct {
	parenDepth   int
	bracketDepth int
	braceDepth   int
	inQuote      byte
}

func (s sqlScanState) isTopLevel() bool {
	return s.inQuote == 0 && s.parenDepth == 0 && s.bracketDepth == 0 && s.braceDepth == 0
}

func (s sqlScanState) isBalanced() bool {
	return s.inQuote == 0 && s.parenDepth == 0 && s.bracketDepth == 0 && s.braceDepth == 0
}

func advanceSQLScanState(raw string, index int, state *sqlScanState) (int, error) {
	ch := raw[index]
	if state.inQuote != 0 {
		switch state.inQuote {
		case '\'':
			if ch == '\\' {
				if index+1 < len(raw) {
					return index + 1, nil
				}
				return index, fmt.Errorf("unterminated escape")
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
				return index, fmt.Errorf("unterminated escape")
			}
			if ch == '"' {
				state.inQuote = 0
			}
		case '`':
			if ch == '\\' {
				if index+1 < len(raw) {
					return index + 1, nil
				}
				return index, fmt.Errorf("unterminated escape")
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
		return index, fmt.Errorf("unbalanced delimiters")
	}

	return index, nil
}

func normalizeIdentifier(value string) string {
	value = normalizeSQL(value)
	value = strings.Trim(value, "`")
	return value
}

func normalizeSQL(value string) string {
	return strings.TrimSpace(value)
}

func columnsEqual(left []dbops.Column, right []dbops.Column) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !columnsExactlyEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func columnsExactlyEqual(left dbops.Column, right dbops.Column) bool {
	return left.Name == right.Name &&
		left.Nullable == right.Nullable &&
		normalizeSQL(left.Type) == normalizeSQL(right.Type) &&
		normalizeSQL(left.Comment) == normalizeSQL(right.Comment) &&
		normalizeOptionalString(left.DefaultExpression) == normalizeOptionalString(right.DefaultExpression) &&
		normalizeOptionalString(left.MaterializedExpression) == normalizeOptionalString(right.MaterializedExpression) &&
		normalizeOptionalString(left.AliasExpression) == normalizeOptionalString(right.AliasExpression)
}

func columnsEqualIgnoringName(left dbops.Column, right dbops.Column) bool {
	left.Name = ""
	right.Name = ""
	return columnsExactlyEqual(left, right)
}

func normalizeOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return normalizeSQL(*value)
}

func hasDuplicateColumnNames(columns []dbops.Column) bool {
	seen := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		if _, exists := seen[column.Name]; exists {
			return true
		}
		seen[column.Name] = struct{}{}
	}
	return false
}

func columnInDesiredPosition(order []string, name string, previousName string) bool {
	index := indexOf(order, name)
	if index == -1 {
		return false
	}
	if previousName == "" {
		return index == 0
	}
	prevIndex := indexOf(order, previousName)
	return prevIndex != -1 && index == prevIndex+1
}

func insertNameAfter(order []string, name string, previousName string) []string {
	order = append([]string{}, order...)
	if previousName == "" {
		return append([]string{name}, order...)
	}
	index := indexOf(order, previousName)
	if index == -1 {
		return append(order, name)
	}
	order = append(order[:index+1], append([]string{name}, order[index+1:]...)...)
	return order
}

func moveNameAfter(order []string, name string, previousName string) []string {
	order = append([]string{}, order...)
	index := indexOf(order, name)
	if index == -1 {
		return order
	}
	order = append(order[:index], order[index+1:]...)
	return insertNameAfter(order, name, previousName)
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
