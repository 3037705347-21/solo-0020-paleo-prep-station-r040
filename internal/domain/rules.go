package domain

import (
	"regexp"
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
)

var catalogPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)

func requiredText(field, value string, max int) (string, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", contracts.Invalid("required_field", field+"不能为空")
	}
	if len([]rune(cleaned)) > max {
		return "", contracts.Invalid("field_too_long", field+"长度超出限制")
	}
	return cleaned, nil
}

func optionalText(field, value string, max int) (string, error) {
	cleaned := strings.TrimSpace(value)
	if len([]rune(cleaned)) > max {
		return "", contracts.Invalid("field_too_long", field+"长度超出限制")
	}
	return cleaned, nil
}

func validateCatalogNumber(value string) (string, error) {
	cleaned, err := requiredText("目录号", value, 64)
	if err != nil {
		return "", err
	}
	if !catalogPattern.MatchString(cleaned) {
		return "", contracts.Invalid(
			"invalid_catalog_number",
			"目录号只能包含字母、数字、点、下划线和连字符",
		)
	}
	return strings.ToUpper(cleaned), nil
}

func validatePercent(value int) error {
	if value < 0 || value > 100 {
		return contracts.Invalid("invalid_percent", "进度比例必须位于 0 到 100 之间")
	}
	return nil
}

const maxCheckpoints = 20

func validateCheckpoints(defs []CheckpointDef) ([]CheckpointDef, error) {
	if len(defs) == 0 {
		return nil, contracts.Invalid("checkpoints_required", "方案必须定义关键阶段检查点")
	}
	if len(defs) > maxCheckpoints {
		return nil, contracts.Invalid("too_many_checkpoints", "检查点数量超出限制")
	}
	cleaned := make([]CheckpointDef, 0, len(defs))
	seen := make(map[string]bool, len(defs))
	previousGate := 0
	for _, def := range defs {
		key, err := requiredText("检查点标识", def.Key, 40)
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, contracts.Invalid("duplicate_checkpoint_key", "检查点标识不能重复")
		}
		seen[key] = true
		name, err := requiredText("检查点名称", def.Name, 80)
		if err != nil {
			return nil, err
		}
		if def.GatePercent < 1 || def.GatePercent > 100 {
			return nil, contracts.Invalid("invalid_gate_percent", "检查点阈值必须位于 1 到 100 之间")
		}
		if def.GatePercent <= previousGate {
			return nil, contracts.Invalid("checkpoint_gate_order", "检查点阈值必须按升序排列")
		}
		previousGate = def.GatePercent
		cleaned = append(cleaned, CheckpointDef{Key: key, Name: name, GatePercent: def.GatePercent})
	}
	if cleaned[len(cleaned)-1].GatePercent != 100 {
		return nil, contracts.Invalid("final_checkpoint_required", "最终检查点阈值必须为 100")
	}
	return cleaned, nil
}
