package domain

import (
	"strings"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type InspectionOutcome string

const (
	InspectionPassed   InspectionOutcome = "passed"
	InspectionRejected InspectionOutcome = "rejected"
)

// InspectionRecord 是质量负责人对某个关键阶段检查点作出的检查结论。
// 记录只追加不修改，检查点的当前状态由最新一条记录决定。
type InspectionRecord struct {
	ID          string            `json:"id"`
	PlanID      string            `json:"plan_id"`
	Checkpoint  string            `json:"checkpoint"`
	Sequence    int               `json:"sequence"`
	Outcome     InspectionOutcome `json:"outcome"`
	InspectedBy string            `json:"inspected_by"`
	Note        string            `json:"note"`
	CreatedAt   time.Time         `json:"created_at"`
}

func NewInspectionRecord(
	id string,
	plan PrepPlan,
	previous []InspectionRecord,
	checkpoint string,
	outcome string,
	inspectedBy string,
	note string,
	now time.Time,
) (InspectionRecord, error) {
	if id == "" {
		return InspectionRecord{}, contracts.Internal("missing_generated_id", "检查标识未生成", nil)
	}
	if plan.Status != PlanApproved && plan.Status != PlanActive {
		return InspectionRecord{}, contracts.Conflict("plan_not_inspectable", "当前方案状态不能记录阶段检查")
	}
	stage, err := requiredText("检查点阶段", checkpoint, 80)
	if err != nil {
		return InspectionRecord{}, err
	}
	index := -1
	for i, spec := range plan.Checkpoints {
		if spec.Stage == stage {
			index = i
			break
		}
	}
	if index < 0 {
		return InspectionRecord{}, contracts.NotFound("checkpoint_not_found", "方案未定义检查点 "+stage)
	}
	var result InspectionOutcome
	switch InspectionOutcome(strings.TrimSpace(outcome)) {
	case InspectionPassed:
		result = InspectionPassed
	case InspectionRejected:
		result = InspectionRejected
	default:
		return InspectionRecord{}, contracts.Invalid("invalid_outcome", "检查结论只能是 passed 或 rejected")
	}
	inspector, err := requiredText("检查人", inspectedBy, 80)
	if err != nil {
		return InspectionRecord{}, err
	}
	noteValue, err := optionalText("检查备注", note, 500)
	if err != nil {
		return InspectionRecord{}, err
	}
	latest := latestInspections(previous)
	for _, spec := range plan.Checkpoints[:index] {
		record, ok := latest[spec.Stage]
		if !ok || record.Outcome != InspectionPassed {
			return InspectionRecord{}, contracts.Conflict("inspection_out_of_order", "必须先通过前一个关键阶段的检查")
		}
	}
	if record, ok := latest[stage]; ok && record.Outcome == InspectionPassed {
		return InspectionRecord{}, contracts.Conflict("checkpoint_already_passed", "该检查点已通过检查，不能重复检查")
	}
	return InspectionRecord{
		ID:          id,
		PlanID:      plan.ID,
		Checkpoint:  stage,
		Sequence:    len(previous) + 1,
		Outcome:     result,
		InspectedBy: inspector,
		Note:        noteValue,
		CreatedAt:   now.UTC(),
	}, nil
}

// latestInspections 以检查点阶段为键返回最新一条检查记录。
// 记录按追加顺序排列，后写入的记录覆盖先写入的。
func latestInspections(records []InspectionRecord) map[string]InspectionRecord {
	latest := make(map[string]InspectionRecord, len(records))
	for _, record := range records {
		latest[record.Checkpoint] = record
	}
	return latest
}
