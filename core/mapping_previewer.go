package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

type MappingPreviewerOption func(*MappingPreviewer)

func WithMappingPreviewerClock(now func() time.Time) MappingPreviewerOption {
	return func(previewer *MappingPreviewer) {
		if previewer == nil || now == nil {
			return
		}
		previewer.now = now
	}
}

type MappingPreviewer struct {
	compiler MappingSpecValidator
	now      func() time.Time
}

func NewMappingPreviewer(
	compiler MappingSpecValidator,
	opts ...MappingPreviewerOption,
) *MappingPreviewer {
	if compiler == nil {
		compiler = NewMappingCompiler()
	}
	previewer := &MappingPreviewer{
		compiler: compiler,
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(previewer)
		}
	}
	return previewer
}

func (p *MappingPreviewer) PreviewMappingSpec(
	ctx context.Context,
	req PreviewMappingSpecRequest,
) (PreviewMappingSpecResult, error) {
	if p == nil || p.compiler == nil {
		return PreviewMappingSpecResult{}, fmt.Errorf("core: mapping previewer compiler is required")
	}

	validationResult, err := p.compiler.ValidateMappingSpec(ctx, ValidateMappingSpecRequest{
		Spec:   req.Spec,
		Schema: req.Schema,
	})
	if err != nil {
		return PreviewMappingSpecResult{}, err
	}

	compiledRules := validationResult.Compiled.Rules
	records := make([]PreviewRecord, 0, len(req.Samples))
	totalRecordIssues := 0
	appliedRuleCount := 0

	for _, sample := range req.Samples {
		output := make(map[string]any)
		diffs := make([]PreviewFieldDiff, 0, len(compiledRules))
		recordIssues := make([]MappingValidationIssue, 0)

		for _, compiledRule := range compiledRules {
			sourceValue, sourceFound := lookupPathValue(sample, compiledRule.Rule.SourcePath)
			if !sourceFound {
				recordIssues = append(recordIssues, mappingIssue(
					"preview_source_missing",
					fmt.Sprintf("core: source value for path %q was not found in sample", compiledRule.Rule.SourcePath),
					compiledRule.Rule.ID,
					compiledRule.Rule.SourcePath,
					compiledRule.Rule.TargetPath,
					MappingValidationIssueWarning,
				))
				continue
			}

			transformed, transformErr := applyMappingTransform(compiledRule.Transform, sourceValue)
			if transformErr != nil {
				recordIssues = append(recordIssues, mappingIssue(
					"preview_transform_failed",
					fmt.Sprintf("core: transform %q failed: %v", compiledRule.Transform, transformErr),
					compiledRule.Rule.ID,
					compiledRule.Rule.SourcePath,
					compiledRule.Rule.TargetPath,
					MappingValidationIssueError,
				))
				continue
			}

			setPathValue(output, compiledRule.Rule.TargetPath, transformed)
			diffs = append(diffs, PreviewFieldDiff{
				RuleID:      compiledRule.Rule.ID,
				SourcePath:  compiledRule.Rule.SourcePath,
				TargetPath:  compiledRule.Rule.TargetPath,
				InputValue:  sourceValue,
				OutputValue: transformed,
				Changed: !reflect.DeepEqual(sourceValue, transformed) ||
					normalizePath(compiledRule.Rule.SourcePath) != normalizePath(compiledRule.Rule.TargetPath),
			})
			appliedRuleCount++
		}

		sort.SliceStable(diffs, func(i, j int) bool {
			left := diffs[i]
			right := diffs[j]
			if left.TargetPath != right.TargetPath {
				return left.TargetPath < right.TargetPath
			}
			if left.SourcePath != right.SourcePath {
				return left.SourcePath < right.SourcePath
			}
			return left.RuleID < right.RuleID
		})
		sortMappingValidationIssues(recordIssues)
		totalRecordIssues += len(recordIssues)

		records = append(records, PreviewRecord{
			Input:  sample,
			Output: output,
			Diff:   diffs,
			Issues: recordIssues,
		})
	}

	report := PreviewReport{
		SampleCount:      len(req.Samples),
		IssueCount:       len(validationResult.Issues) + totalRecordIssues,
		AppliedRuleCount: appliedRuleCount,
	}
	deterministicHash, hashErr := previewDeterministicHash(validationResult.Issues, records, validationResult.Compiled.DeterministicHash, report)
	if hashErr != nil {
		return PreviewMappingSpecResult{}, hashErr
	}

	return PreviewMappingSpecResult{
		Issues:            validationResult.Issues,
		Records:           records,
		Report:            report,
		DeterministicHash: deterministicHash,
		GeneratedAt:       p.now(),
	}, nil
}

func previewDeterministicHash(
	issues []MappingValidationIssue,
	records []PreviewRecord,
	compiledHash string,
	report PreviewReport,
) (string, error) {
	payload, err := json.Marshal(struct {
		CompiledHash string                   `json:"compiled_hash"`
		Issues       []MappingValidationIssue `json:"issues"`
		Records      []PreviewRecord          `json:"records"`
		Report       PreviewReport            `json:"report"`
	}{
		CompiledHash: compiledHash,
		Issues:       issues,
		Records:      records,
		Report:       report,
	})
	if err != nil {
		return "", fmt.Errorf("core: marshal preview payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func lookupPathValue(root map[string]any, path string) (any, bool) {
	if root == nil {
		return nil, false
	}
	parts := strings.Split(normalizePath(path), ".")
	current := any(root)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := asMap[part]
		if !exists {
			return nil, false
		}
		current = next
	}
	return current, true
}

func setPathValue(root map[string]any, path string, value any) {
	parts := strings.Split(normalizePath(path), ".")
	if len(parts) == 0 {
		return
	}
	current := root
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		if idx == len(parts)-1 {
			current[part] = value
			return
		}
		next, exists := current[part]
		if !exists {
			child := make(map[string]any)
			current[part] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = make(map[string]any)
			current[part] = child
		}
		current = child
	}
}
