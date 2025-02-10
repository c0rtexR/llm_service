package provider

import (
	"github.com/c0rtexR/llm_service/internal/provider"
	"github.com/c0rtexR/llm_service/internal/provider/anthropic"
	"github.com/c0rtexR/llm_service/internal/provider/gemini"
	"github.com/c0rtexR/llm_service/internal/provider/openai"
	"github.com/c0rtexR/llm_service/internal/provider/openrouter"
)

// Re-export the LLMProvider interface
type LLMProvider = provider.LLMProvider

// Re-export the Config struct
type Config = provider.Config

// Factory functions for creating providers
func NewOpenAI(cfg *Config) LLMProvider {
	return openai.New(cfg)
}

func NewAnthropic(cfg *Config) LLMProvider {
	return anthropic.New(cfg)
}

func NewGemini(cfg *Config) (LLMProvider, error) {
	return gemini.New(cfg)
}

func NewOpenRouter(cfg *Config) LLMProvider {
	return openrouter.New(cfg)
}
