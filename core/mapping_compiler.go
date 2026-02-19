package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type MappingCompiler struct{}

func NewMappingCompiler() *MappingCompiler {
	return &MappingCompiler{}
}

func (c *MappingCompiler) ValidateMappingSpec(
	ctx context.Context,
	req ValidateMappingSpecRequest,
) (ValidateMappingSpecResult, error) {
	compiled, issues, err := c.CompileMappingSpec(ctx, req)
	if err != nil {
		return ValidateMappingSpecResult{}, err
	}
	normalizedSpec := normalizeMappingSpec(req.Spec)
	sortMappingValidationIssues(issues)
	return ValidateMappingSpecResult{
		Valid:          !containsMappingErrors(issues),
		Issues:         issues,
		NormalizedSpec: normalizedSpec,
		Compiled:       compiled,
	}, nil
}

func (c *MappingCompiler) CompileMappingSpec(
	_ context.Context,
	req ValidateMappingSpecRequest,
) (CompiledMappingSpec, []MappingValidationIssue, error) {
	if c == nil {
		return CompiledMappingSpec{}, nil, fmt.Errorf("core: mapping compiler is required")
	}

	spec := normalizeMappingSpec(req.Spec)
	var issues []MappingValidationIssue

	if err := req.Schema.Validate(); err != nil {
		issues = append(issues, mappingIssue("invalid_schema", err.Error(), "", "", "", MappingValidationIssueError))
	}
	if err := spec.Validate(); err != nil {
		issues = append(issues, mappingIssue("invalid_spec", err.Error(), "", "", "", MappingValidationIssueError))
	}
	schemaReference := deriveExternalSchemaReference(req.Schema)
	if schemaReference != "" && strings.TrimSpace(spec.SchemaRef) != "" {
		if normalizePath(schemaReference) != normalizePath(spec.SchemaRef) {
			issues = append(issues, mappingIssue(
				"schema_drift_detected",
				fmt.Sprintf(
					"core: mapping spec schema_ref %q differs from active schema %q",
					spec.SchemaRef,
					schemaReference,
				),
				"",
				"",
				"",
				MappingValidationIssueWarning,
			))
		}
	}

	sourceObject, sourceObjectFound := findExternalObject(req.Schema, spec.SourceObject)
	if !sourceObjectFound {
		issues = append(issues, mappingIssue(
			"source_object_not_found",
			fmt.Sprintf("core: source object %q not found in schema", spec.SourceObject),
			"",
			"",
			"",
			MappingValidationIssueError,
		))
		sortMappingValidationIssues(issues)
		return CompiledMappingSpec{
			SpecID:       spec.SpecID,
			Version:      spec.Version,
			SourceObject: spec.SourceObject,
		}, issues, nil
	}

	fieldByPath, requiredFields := buildSourceFieldIndexes(sourceObject)
	mappedRequiredFields := make(map[string]struct{})
	targetPathToRuleID := make(map[string]string)
	compiledRules := make([]CompiledMappingRule, 0, len(spec.Rules))

	for _, rule := range spec.Rules {
		rule = normalizeMappingRule(rule)
		sourcePath := normalizePath(rule.SourcePath)
		targetPath := normalizePath(rule.TargetPath)

		sourceField, sourceFound := fieldByPath[sourcePath]
		if !sourceFound {
			issues = append(issues, mappingIssue(
				"source_field_not_found",
				fmt.Sprintf("core: source field %q not found in object %q", rule.SourcePath, sourceObject.Name),
				rule.ID,
				rule.SourcePath,
				rule.TargetPath,
				MappingValidationIssueError,
			))
		} else if sourceField.Required {
			mappedRequiredFields[sourcePath] = struct{}{}
		}

		transform := normalizeTransform(rule.Transform)
		transformSupported := isSupportedMappingTransform(transform)
		if !transformSupported {
			issues = append(issues, mappingIssue(
				"transform_unknown",
				fmt.Sprintf("core: unsupported transform %q", rule.Transform),
				rule.ID,
				rule.SourcePath,
				rule.TargetPath,
				MappingValidationIssueError,
			))
		}

		targetType := resolveTargetType(rule)
		sourceType := canonicalFieldType(sourceField.Type)
		if transformSupported &&
			sourceFound &&
			targetType != "" &&
			!isMappingTypeCompatible(sourceType, targetType, transform) {
			issues = append(issues, mappingIssue(
				"type_incompatible",
				fmt.Sprintf(
					"core: source type %q is not compatible with target type %q using transform %q",
					sourceType,
					targetType,
					transform,
				),
				rule.ID,
				rule.SourcePath,
				rule.TargetPath,
				MappingValidationIssueError,
			))
		}

		if existingRuleID, duplicate := targetPathToRuleID[targetPath]; duplicate {
			issues = append(issues, mappingIssue(
				"target_path_duplicate",
				fmt.Sprintf("core: duplicate target path %q for rules %q and %q", rule.TargetPath, existingRuleID, rule.ID),
				rule.ID,
				rule.SourcePath,
				rule.TargetPath,
				MappingValidationIssueError,
			))
		} else if targetPath != "" {
			targetPathToRuleID[targetPath] = rule.ID
		}

		compiledRules = append(compiledRules, CompiledMappingRule{
			Rule:       rule,
			SourceType: sourceType,
			TargetType: targetType,
			Transform:  transform,
		})
	}

	requiredPaths := make([]string, 0, len(requiredFields))
	for path := range requiredFields {
		requiredPaths = append(requiredPaths, path)
	}
	sort.Strings(requiredPaths)
	for _, path := range requiredPaths {
		if _, ok := mappedRequiredFields[path]; ok {
			continue
		}
		issues = append(issues, mappingIssue(
			"required_field_unmapped",
			fmt.Sprintf("core: required source field %q is not mapped", requiredFields[path].Path),
			"",
			requiredFields[path].Path,
			"",
			MappingValidationIssueError,
		))
	}

	sort.SliceStable(compiledRules, func(i, j int) bool {
		left := compiledRules[i]
		right := compiledRules[j]
		if left.Rule.TargetPath != right.Rule.TargetPath {
			return left.Rule.TargetPath < right.Rule.TargetPath
		}
		if left.Rule.SourcePath != right.Rule.SourcePath {
			return left.Rule.SourcePath < right.Rule.SourcePath
		}
		return left.Rule.ID < right.Rule.ID
	})

	compiled := CompiledMappingSpec{
		SpecID:       spec.SpecID,
		Version:      spec.Version,
		SourceObject: spec.SourceObject,
		Rules:        compiledRules,
	}

	hash, err := mappingCompiledHash(compiled)
	if err != nil {
		return CompiledMappingSpec{}, nil, err
	}
	compiled.DeterministicHash = hash

	sortMappingValidationIssues(issues)
	return compiled, issues, nil
}

func containsMappingErrors(issues []MappingValidationIssue) bool {
	for _, issue := range issues {
		if issue.Severity == MappingValidationIssueError {
			return true
		}
	}
	return false
}

func sortMappingValidationIssues(issues []MappingValidationIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left := issues[i]
		right := issues[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if left.SourcePath != right.SourcePath {
			return left.SourcePath < right.SourcePath
		}
		if left.TargetPath != right.TargetPath {
			return left.TargetPath < right.TargetPath
		}
		return left.Message < right.Message
	})
}

func mappingIssue(
	code string,
	message string,
	ruleID string,
	sourcePath string,
	targetPath string,
	severity MappingValidationIssueSeverity,
) MappingValidationIssue {
	return MappingValidationIssue{
		Code:       strings.TrimSpace(strings.ToLower(code)),
		Message:    strings.TrimSpace(message),
		Severity:   severity,
		RuleID:     strings.TrimSpace(ruleID),
		SourcePath: strings.TrimSpace(sourcePath),
		TargetPath: strings.TrimSpace(targetPath),
	}
}

func mappingCompiledHash(compiled CompiledMappingSpec) (string, error) {
	payload, err := json.Marshal(compiled)
	if err != nil {
		return "", fmt.Errorf("core: marshal compiled mapping spec: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func buildSourceFieldIndexes(
	object ExternalObjectSchema,
) (map[string]ExternalField, map[string]ExternalField) {
	fields := make(map[string]ExternalField, len(object.Fields))
	required := make(map[string]ExternalField)
	for _, field := range object.Fields {
		path := normalizePath(field.Path)
		fields[path] = field
		if field.Required {
			required[path] = field
		}
	}
	return fields, required
}

func findExternalObject(schema ExternalSchema, objectName string) (ExternalObjectSchema, bool) {
	target := normalizePath(objectName)
	for _, object := range schema.Objects {
		if normalizePath(object.Name) == target {
			return object, true
		}
	}
	return ExternalObjectSchema{}, false
}

func deriveExternalSchemaReference(schema ExternalSchema) string {
	if strings.TrimSpace(schema.ID) != "" {
		return strings.TrimSpace(schema.ID)
	}
	name := strings.TrimSpace(schema.Name)
	version := strings.TrimSpace(schema.Version)
	if name == "" {
		return ""
	}
	if version == "" {
		return name
	}
	return name + "@" + version
}

func normalizeMappingRule(rule MappingRule) MappingRule {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.SourcePath = normalizePath(rule.SourcePath)
	rule.TargetPath = normalizePath(rule.TargetPath)
	rule.Transform = normalizeTransform(rule.Transform)
	return rule
}

func normalizePath(path string) string {
	return strings.TrimSpace(path)
}

func normalizeTransform(transform string) string {
	candidate := strings.TrimSpace(strings.ToLower(transform))
	if candidate == "" {
		return "identity"
	}
	return candidate
}

func resolveTargetType(rule MappingRule) string {
	if rule.Constraints == nil {
		return ""
	}
	for _, key := range []string{"target_type", "targetType", "type"} {
		raw, ok := rule.Constraints[key]
		if !ok {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			continue
		}
		return canonicalFieldType(text)
	}
	return ""
}

func canonicalFieldType(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "int", "integer":
		return "integer"
	case "float", "double", "decimal", "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "str", "string", "text":
		return "string"
	case "timestamp", "datetime", "date_time":
		return "datetime"
	default:
		return value
	}
}

func isSupportedMappingTransform(transform string) bool {
	switch transform {
	case "identity",
		"to_string",
		"to_int",
		"to_float",
		"to_bool",
		"trim",
		"lowercase",
		"uppercase",
		"unix_time_to_rfc3339":
		return true
	default:
		return false
	}
}

func isMappingTypeCompatible(sourceType, targetType, transform string) bool {
	targetType = canonicalFieldType(targetType)
	sourceType = canonicalFieldType(sourceType)
	if targetType == "" || sourceType == "" {
		return true
	}

	switch normalizeTransform(transform) {
	case "identity":
		return sourceType == targetType
	case "to_string":
		return targetType == "string"
	case "to_int":
		return targetType == "integer" &&
			(sourceType == "string" || sourceType == "integer" || sourceType == "number" || sourceType == "boolean")
	case "to_float":
		return targetType == "number" &&
			(sourceType == "string" || sourceType == "integer" || sourceType == "number")
	case "to_bool":
		return targetType == "boolean" &&
			(sourceType == "string" || sourceType == "integer" || sourceType == "boolean")
	case "trim", "lowercase", "uppercase":
		return sourceType == "string" && targetType == "string"
	case "unix_time_to_rfc3339":
		return targetType == "string" &&
			(sourceType == "integer" || sourceType == "number" || sourceType == "string")
	default:
		return false
	}
}
