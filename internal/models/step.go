package models

import (
	"encoding/json"
)

// Step 代表一个动作步骤，定义所有支持的步骤类型
// 对应 definition JSON 中的单个步骤对象
type Step struct {
	// ---- 通用字段 ----
	// Type 步骤类型: open | click | input | delay | waitSelector | hasText |
	//                extract | log | condition | loop | screenshot | getSource | js
	Type string `json:"type"`

	// TimeoutSec 步骤超时秒数（适用于 open/click/input/waitSelector 等需要等待的场景）
	TimeoutSec int `json:"timeoutSec"`

	// ---- open: 打开网页 ----
	URL string `json:"url,omitempty"`

	// ---- click | input | waitSelector | hasText | extract | getSource: 元素选择器 ----
	Selector string `json:"selector,omitempty"`

	// ---- input: 文本输入 ----
	// Text 要输入的文本内容，支持 {{varName}} 变量插值
	Text string `json:"text,omitempty"`
	// CharDelayMs 每个字符输入间隔（毫秒），模拟人类输入节奏
	CharDelayMs int `json:"charDelayMs,omitempty"`
	// Clear 输入前是否清空已有内容
	Clear bool `json:"clear,omitempty"`

	// ---- delay: 等待 ----
	// Sec 固定等待秒数
	Sec int `json:"sec,omitempty"`
	// MinSec 随机等待最小秒数（与 MaxSec 配合使用）
	MinSec int `json:"minSec,omitempty"`
	// MaxSec 随机等待最大秒数
	MaxSec int `json:"maxSec,omitempty"`

	// ---- hasText: 检查文本是否存在 ----
	// Text2 与 Selector 配合，检查元素内是否包含指定文本
	// 注意：此字段已映射到上方的 Text，兼容两种字段名
	Text2 string `json:"text2,omitempty"`

	// ---- extract: 提取数据 ----
	// Var 变量名，提取后存入执行上下文
	Var string `json:"var,omitempty"`
	// Attr 提取属性（如 "value", "href", "src" 等），为空则提取文本内容
	Attr string `json:"attr,omitempty"`

	// ---- log: 输出日志 ----
	// Message 日志内容，支持 {{varName}} 变量插值
	Message string `json:"message,omitempty"`

	// ---- condition: 条件分支 ----
	// If 条件判断步骤（如 hasText 类型的 Step）
	If *Step `json:"if,omitempty"`
	// Then 条件为真时执行的步骤列表
	Then []Step `json:"then,omitempty"`
	// Else 条件为假时执行的步骤列表
	Else []Step `json:"else,omitempty"`

	// ---- loop: 循环执行 ----
	// Count 循环次数（与 Until 二选一）
	Count int `json:"count,omitempty"`
	// Until 循环终止条件（与 Count 二选一），为 nil 时按 Count 执行
	Until *Step `json:"until,omitempty"`
	// Steps 循环体中的步骤列表
	Steps []Step `json:"steps,omitempty"`

	// ---- screenshot: 截图 ----
	// Path 截图保存路径（相对 UploadDir），默认自动命名
	Path string `json:"path,omitempty"`
	// FullPage 是否截取整页（true: 整页滚动截图，false: 当前视口）
	FullPage bool `json:"fullPage,omitempty"`

	// ---- getSource: 获取渲染后 HTML 源码 ----
	// Selector2 可选，指定要获取源码的元素选择器；为空则获取整页 HTML
	Selector2 string `json:"selector2,omitempty"`

	// ---- js: 执行自定义 JavaScript ----
	Script string `json:"script,omitempty"`
}

// GetText 返回步骤中的文本内容，兼容 Text 和 Text2 两个字段
func (s *Step) GetText() string {
	if s.Text != "" {
		return s.Text
	}
	return s.Text2
}

// MarshalJSON 实现 json.Marshaler，支持自定义序列化逻辑
func (s Step) MarshalJSON() ([]byte, error) {
	// 使用类型别名避免递归调用 MarshalJSON
	type Alias Step
	aux := struct {
		Alias
	}{
		Alias: (Alias)(s),
	}
	return json.Marshal(aux)
}

// UnmarshalJSON 实现 json.Unmarshaler，支持 Text2 映射到 Text
func (s *Step) UnmarshalJSON(data []byte) error {
	type Alias Step
	aux := &struct {
		Text2 string `json:"text2"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	// 兼容 text2 字段，自动映射到 Text
	if aux.Text2 != "" && s.Text == "" {
		s.Text = aux.Text2
	}
	return nil
}

// Validate 验证 Step 必填字段（基础校验，不做深度验证）
func (s *Step) Validate() error {
	if s.Type == "" {
		return &StepValidationError{Field: "type", Message: "step type is required"}
	}
	validTypes := map[string]bool{
		"open":         true,
		"click":        true,
		"input":        true,
		"delay":        true,
		"waitSelector": true,
		"hasText":      true,
		"extract":      true,
		"log":          true,
		"condition":    true,
		"loop":         true,
		"screenshot":   true,
		"getSource":    true,
		"js":           true,
	}
	if !validTypes[s.Type] {
		return &StepValidationError{Field: "type", Message: "unsupported step type: " + s.Type}
	}
	return nil
}

// StepValidationError 步骤校验错误
type StepValidationError struct {
	Field   string
	Message string
}

func (e *StepValidationError) Error() string {
	return "validation error on field '" + e.Field + "': " + e.Message
}
