package main

import (
	"encoding/json"
	"errors"
	"strings"

	"campusclaw/internal/mcp"
)

var queryInput = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500}},"required":["query"],"additionalProperties":false}`)

func teachingTools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "formula_hint", Description: "只读教学提示：根据问题提示公式适用条件、符号和下一步检查；不计算练习的最终数值答案。query 为 1–500 字符的非空问题。", InputSchema: queryInput},
		{Name: "unit_convert", Description: "只读单位提示：提供长度、质量或时间的单位关系、量纲和换算方向；不代算具体练习的最终数值答案。query 为 1–500 字符的非空问题。", InputSchema: queryInput},
	}
}

func callTeachingTool(name string, arguments json.RawMessage) (mcp.CallResult, error) {
	if name != "formula_hint" && name != "unit_convert" {
		return mcp.CallResult{}, errors.New("unknown teaching tool")
	}
	validator, err := mcp.CompileInputSchema(queryInput)
	if err != nil || validator.Validate(arguments) != nil {
		return mcp.CallResult{}, errors.New("invalid teaching query")
	}
	var args struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(arguments, &args) != nil || strings.TrimSpace(args.Query) == "" {
		return mcp.CallResult{}, errors.New("invalid teaching query")
	}
	query := strings.ToLower(args.Query)
	text := formulaHint(query)
	if name == "unit_convert" {
		text = unitHint(query)
	}
	return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: text}}, IsError: false}, nil
}

func formulaHint(query string) string {
	switch {
	case strings.Contains(query, "括号") || strings.Contains(query, "分配律"):
		return "先检查括号前的符号：前面是减号时，去括号要改变括号内每一项的符号；有乘法时，用分配律逐项相乘。请先写出变形关系，再自己计算并检查。"
	case strings.Contains(query, "勾股") || strings.Contains(query, "直角"):
		return "先确认这是直角三角形，再找直角所对的斜边。用 a² + b² = c² 时，c 必须代表斜边，三条边须统一单位。请先标出已知边与未知边，再列关系式，不急着计算。"
	default:
		return "先圈出已知量、待求量及各自单位，再检查公式的适用条件；把题目中的量对应到公式符号。请先列关系并说明为何适用，再自己完成计算。"
	}
}

func unitHint(query string) string {
	switch {
	case strings.Contains(query, "时间") || strings.Contains(query, "小时") || strings.Contains(query, "分钟") || strings.Contains(query, "秒"):
		return "时间关系：1 小时 = 60 分钟，1 分钟 = 60 秒。先选统一单位；从大单位到小单位乘进率，反向则除以进率。请列出换算式并检查量纲，再自行计算。"
	case strings.Contains(query, "质量") || strings.Contains(query, "千克") || strings.Contains(query, "克") || strings.Contains(query, "吨"):
		return "质量关系：1 吨 = 1000 千克，1 千克 = 1000 克。先确定目标单位；大单位化小单位乘进率，反向除以进率。请保留单位写出换算步骤，自己完成数值运算。"
	default:
		return "长度关系：1 千米 = 1000 米，1 米 = 100 厘米，1 厘米 = 10 毫米。大单位化小单位乘进率，反向除以进率；面积或体积须分别考虑进率的平方或立方。请先列含单位的关系，不直接代算得数。"
	}
}
