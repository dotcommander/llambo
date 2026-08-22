package evals

import _ "embed"

//go:embed prompts/generation-system.txt
var writingGenerationSystemPrompt string

//go:embed prompts/judge-system.txt
var writingJudgeSystemPrompt string

//go:embed prompts/judge-user.txt
var writingJudgeUserTemplate string

//go:embed prompts/judge-combined-system.txt
var writingCombinedJudgeSystemPrompt string

//go:embed prompts/judge-combined-user.txt
var writingCombinedJudgeUserTemplate string

//go:embed prompts/judge-combined-schema.json
var writingCombinedJudgeSchema []byte
