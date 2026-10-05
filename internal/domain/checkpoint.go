package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// CheckpointSpec 是方案在创建时定义的关键阶段检查点。
// 进度比例达到或越过 Percent 之前，该检查点必须已经通过检查。
type CheckpointSpec struct {
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
}

const maxCheckpointsPerPlan = 32

func validateCheckpoints(specs []CheckpointSpec) ([]CheckpointSpec, error) {
	if len(specs) > maxCheckpointsPerPlan {
		return nil, contracts.Invalid("too_many_checkpoints", "关键阶段检查点数量超出限制")
	}
	cleaned := make([]CheckpointSpec, 0, len(specs))
	seen := make(map[string]bool, len(specs))
	lastPercent := 0
	for _, spec := range specs {
		stage, err := requiredText("检查点阶段", spec.Stage, 80)
		if err != nil {
			return nil, err
		}
		if spec.Percent < 1 || spec.Percent > 100 {
			return nil, contracts.Invalid("invalid_checkpoint_percent", "检查点比例必须位于 1 到 100 之间")
		}
		if spec.Percent <= lastPercent {
			return nil, contracts.Invalid("checkpoint_percent_order", "检查点比例必须严格递增")
		}
		if seen[stage] {
			return nil, contracts.Invalid("duplicate_checkpoint", "检查点阶段不能重复")
		}
		seen[stage] = true
		lastPercent = spec.Percent
		cleaned = append(cleaned, CheckpointSpec{Stage: stage, Percent: spec.Percent})
	}
	return cleaned, nil
}

type CheckpointState string

const (
	CheckpointPending  CheckpointState = "pending"
	CheckpointPassed   CheckpointState = "passed"
	CheckpointRejected CheckpointState = "rejected"
)

// CheckpointStatus 是检查点结合最新检查记录后的公开投影。
type CheckpointStatus struct {
	Stage       string          `json:"stage"`
	Percent     int             `json:"percent"`
	Final       bool            `json:"final"`
	State       CheckpointState `json:"state"`
	InspectedBy string          `json:"inspected_by,omitempty"`
	InspectedAt *time.Time      `json:"inspected_at,omitempty"`
}

// CheckpointStatuses 按定义顺序返回每个检查点的当前状态，
// 让质量负责人可以公开确认各关键阶段是否已检查、由谁检查以及检查结论。
func (p PrepPlan) CheckpointStatuses(inspections []InspectionRecord) []CheckpointStatus {
	latest := latestInspections(inspections)
	statuses := make([]CheckpointStatus, 0, len(p.Checkpoints))
	for i, spec := range p.Checkpoints {
		status := CheckpointStatus{
			Stage:   spec.Stage,
			Percent: spec.Percent,
			Final:   i == len(p.Checkpoints)-1,
			State:   CheckpointPending,
		}
		if record, ok := latest[spec.Stage]; ok {
			status.State = CheckpointState(record.Outcome)
			status.InspectedBy = record.InspectedBy
			stamp := record.CreatedAt
			status.InspectedAt = &stamp
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// FirstBlockingCheckpoint 返回比例 percent 触及但尚未通过检查的第一个检查点。
// 比例达到一百时会覆盖全部检查点，因此方案完成前最终检查必须通过。
func (p PrepPlan) FirstBlockingCheckpoint(percent int, inspections []InspectionRecord) (CheckpointSpec, bool) {
	latest := latestInspections(inspections)
	for _, spec := range p.Checkpoints {
		if spec.Percent > percent {
			break
		}
		record, ok := latest[spec.Stage]
		if !ok || record.Outcome != InspectionPassed {
			return spec, true
		}
	}
	return CheckpointSpec{}, false
}
