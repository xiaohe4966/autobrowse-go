package models

import (
	"encoding/json"
)

// TaskDefinition 任务定义根结构，对应 tasks.definition JSON 字段
type TaskDefinition struct {
	// Steps 步骤列表，按顺序执行
	Steps []Step `json:"steps"`
	// Retry 重试配置（可选）
	Retry *RetryConfig `json:"retry,omitempty"`
	// OnFailure 失败时处理配置（可选）
	OnFailure *OnFailureConfig `json:"onFailure,omitempty"`
}

// RetryConfig 任务级重试配置
type RetryConfig struct {
	// MaxAttempts 最大重试次数（含首次执行），默认 1（即不重试）
	MaxAttempts int `json:"maxAttempts"`
	// DelaySec 每次重试前的等待秒数，默认 30
	DelaySec int `json:"delaySec"`
	// OnErrors 触发重试的错误类型列表，为空表示所有错误都重试
	// 支持的错误类型：selector_not_found | timeout | navigation_failed | element_not_interactable
	OnErrors []string `json:"onErrors"`
}

// OnFailureConfig 任务失败时的处理配置
type OnFailureConfig struct {
	// Screenshot 失败时是否自动截图
	Screenshot bool `json:"screenshot"`
	// LogVariables 失败时是否记录当前所有变量的快照
	LogVariables bool `json:"logVariables"`
}

// ParseTaskDefinition 将 JSON 字节解析为 TaskDefinition 结构体
func ParseTaskDefinition(data []byte) (*TaskDefinition, error) {
	if len(data) == 0 {
		return nil, &DefinitionParseError{Message: "definition data is empty"}
	}
	if !json.Valid(data) {
		return nil, &DefinitionParseError{Message: "invalid JSON format"}
	}
	var def TaskDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, &DefinitionParseError{Message: "JSON parse error: " + err.Error()}
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return &def, nil
}

// Validate 验证 TaskDefinition 的整体结构
func (d *TaskDefinition) Validate() error {
	if len(d.Steps) == 0 {
		return &DefinitionValidationError{Message: "at least one step is required"}
	}
	// 递归校验每个步骤
	for i, step := range d.Steps {
		if err := step.Validate(); err != nil {
			return &DefinitionValidationError{
				Message: "step " + itoa(i) + " validation failed: " + err.Error(),
			}
		}
		// 递归校验 condition/loop 嵌套步骤
		if err := d.validateNestedSteps(step.Then, i, "then"); err != nil {
			return err
		}
		if err := d.validateNestedSteps(step.Else, i, "else"); err != nil {
			return err
		}
		if err := d.validateNestedSteps(step.Steps, i, "steps"); err != nil {
			return err
		}
		if step.If != nil {
			if err := step.If.Validate(); err != nil {
				return &DefinitionValidationError{
					Message: "step " + itoa(i) + " (condition.if) validation failed: " + err.Error(),
				}
			}
		}
	}
	// 校验 Retry 配置
	if d.Retry != nil {
		if d.Retry.MaxAttempts < 1 {
			return &DefinitionValidationError{Message: "retry.maxAttempts must be >= 1"}
		}
		if d.Retry.MaxAttempts > 10 {
			return &DefinitionValidationError{Message: "retry.maxAttempts must be <= 10"}
		}
	}
	return nil
}

// validateNestedSteps 递归校验嵌套步骤列表
func (d *TaskDefinition) validateNestedSteps(steps []Step, parentIdx int, fieldName string) error {
	for i, step := range steps {
		if err := step.Validate(); err != nil {
			return &DefinitionValidationError{
				Message: "step " + itoa(parentIdx) + "." + fieldName + "[" + itoa(i) + "] validation failed: " + err.Error(),
			}
		}
		if err := d.validateNestedSteps(step.Then, parentIdx, fieldName+".then"); err != nil {
			return err
		}
		if err := d.validateNestedSteps(step.Else, parentIdx, fieldName+".else"); err != nil {
			return err
		}
		if err := d.validateNestedSteps(step.Steps, parentIdx, fieldName+".steps"); err != nil {
			return err
		}
	}
	return nil
}

// DefinitionParseError JSON 解析错误
type DefinitionParseError struct {
	Message string
}

func (e *DefinitionParseError) Error() string {
	return "definition parse error: " + e.Message
}

// DefinitionValidationError 校验错误
type DefinitionValidationError struct {
	Message string
}

func (e *DefinitionValidationError) Error() string {
	return "definition validation error: " + e.Message
}

// itoa 简单的 int 转 string（避免引入 strconv 增加依赖）
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	result := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		result = string(rune('0'+i%10)) + result
		i /= 10
	}
	if neg {
		result = "-" + result
	}
	return result
}
