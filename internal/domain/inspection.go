package domain

import (
	"strings"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type InspectionResult string

const (
	InspectionPassed   InspectionResult = "passed"
	InspectionRejected InspectionResult = "rejected"
)

const (
	CheckpointPending  = "pending"
	CheckpointPassed   = "passed"
	CheckpointRejected = "rejected"
)

type Inspection struct {
	ID            string           `json:"id"`
	PlanID        string           `json:"plan_id"`
	CheckpointKey string           `json:"checkpoint_key"`
	Sequence      int              `json:"sequence"`
	Result        InspectionResult `json:"result"`
	InspectedBy   string           `json:"inspected_by"`
	Note          string           `json:"note"`
	CreatedAt     time.Time        `json:"created_at"`
}

func NewInspection(
	id string,
	plan PrepPlan,
	checkpointKey string,
	result string,
	inspectedBy string,
	note string,
	previous []Inspection,
	now time.Time,
) (Inspection, error) {
	if id == "" {
		return Inspection{}, contracts.Internal("missing_generated_id", "检查标识未生成", nil)
	}
	if plan.Status != PlanApproved && plan.Status != PlanActive {
		return Inspection{}, contracts.Conflict("plan_not_inspectable", "当前方案状态不能登记检查")
	}
	key, err := requiredText("检查点", checkpointKey, 40)
	if err != nil {
		return Inspection{}, err
	}
	defined := false
	for _, def := range plan.Checkpoints {
		if def.Key == key {
			defined = true
			break
		}
	}
	if !defined {
		return Inspection{}, contracts.NotFound("checkpoint_not_found", "方案未定义检查点 "+key)
	}
	outcome := InspectionResult(strings.TrimSpace(result))
	if outcome != InspectionPassed && outcome != InspectionRejected {
		return Inspection{}, contracts.Invalid("invalid_inspection_result", "检查结果只能是 passed 或 rejected")
	}
	inspector, err := requiredText("检查人", inspectedBy, 80)
	if err != nil {
		return Inspection{}, err
	}
	conclusion, err := requiredText("检查结论", note, 500)
	if err != nil {
		return Inspection{}, err
	}
	latest := latestInspections(previous)
	if entry, ok := latest[key]; ok && entry.Result == InspectionPassed {
		return Inspection{}, contracts.Conflict("checkpoint_already_passed", "该检查点已通过，不能重复检查")
	}
	for _, def := range plan.Checkpoints {
		if def.Key == key {
			break
		}
		entry, ok := latest[def.Key]
		if !ok || entry.Result != InspectionPassed {
			return Inspection{}, contracts.Conflict("checkpoint_out_of_order", "必须先通过前置检查点 "+def.Name)
		}
	}
	return Inspection{
		ID:            id,
		PlanID:        plan.ID,
		CheckpointKey: key,
		Sequence:      len(previous) + 1,
		Result:        outcome,
		InspectedBy:   inspector,
		Note:          conclusion,
		CreatedAt:     now.UTC(),
	}, nil
}

type CheckpointState struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	GatePercent int        `json:"gate_percent"`
	Status      string     `json:"status"`
	Result      string     `json:"result,omitempty"`
	InspectedBy string     `json:"inspected_by,omitempty"`
	Note        string     `json:"note,omitempty"`
	InspectedAt *time.Time `json:"inspected_at,omitempty"`
}

func (p PrepPlan) CheckpointStates(inspections []Inspection) []CheckpointState {
	latest := latestInspections(inspections)
	states := make([]CheckpointState, 0, len(p.Checkpoints))
	for _, def := range p.Checkpoints {
		state := CheckpointState{
			Key:         def.Key,
			Name:        def.Name,
			GatePercent: def.GatePercent,
			Status:      CheckpointPending,
		}
		if entry, ok := latest[def.Key]; ok {
			state.Result = string(entry.Result)
			state.InspectedBy = entry.InspectedBy
			state.Note = entry.Note
			stamp := entry.CreatedAt
			state.InspectedAt = &stamp
			if entry.Result == InspectionPassed {
				state.Status = CheckpointPassed
			} else {
				state.Status = CheckpointRejected
			}
		}
		states = append(states, state)
	}
	return states
}

func (p PrepPlan) EnsureProgressAllowed(inspections []Inspection, percent int) error {
	states := p.CheckpointStates(inspections)
	for _, state := range states {
		if state.Status == CheckpointRejected {
			return contracts.Conflict("checkpoint_rejected", "检查结论为退回，重新检查通过前不能继续登记进度")
		}
	}
	for _, state := range states {
		if state.Status != CheckpointPassed && percent > state.GatePercent {
			return contracts.Conflict("checkpoint_not_passed", "进度不能越过尚未通过的检查点 "+state.Name)
		}
	}
	if percent == 100 {
		for _, state := range states {
			if state.Status != CheckpointPassed {
				return contracts.Conflict("final_checkpoint_not_passed", "完成方案前必须通过最终检查")
			}
		}
	}
	return nil
}

func latestInspections(previous []Inspection) map[string]Inspection {
	latest := make(map[string]Inspection, len(previous))
	for _, entry := range previous {
		latest[entry.CheckpointKey] = entry
	}
	return latest
}
