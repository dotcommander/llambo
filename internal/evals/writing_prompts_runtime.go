package evals

import _ "embed"

//go:embed prompts/generation-system.txt
var writingGenerationSystemPrompt string

//go:embed prompts/judge-system.txt
var writingJudgeSystemPrompt string

//go:embed prompts/judge-user.txt
var writingJudgeUserTemplate string
