package main

import (
	"strings"

	"github.com/spf13/viper"
)

func llmProviderFromViper() string {
	return strings.TrimSpace(viper.GetString("llm.provider"))
}

func llmEndpointFromViper() string {
	return llmEndpointForProvider(llmProviderFromViper())
}

func llmAPIKeyFromViper() string {
	return llmAPIKeyForProvider(llmProviderFromViper())
}

func llmModelFromViper() string {
	return llmModelForProvider(llmProviderFromViper())
}

func llmEndpointForProvider(provider string) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "azure":
		return firstNonEmpty(viper.GetString("llm.azure.endpoint"), viper.GetString("llm.endpoint"))
	default:
		return strings.TrimSpace(viper.GetString("llm.endpoint"))
	}
}

func llmAPIKeyForProvider(provider string) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "azure":
		return firstNonEmpty(viper.GetString("llm.azure.api_key"), viper.GetString("llm.api_key"))
	default:
		return strings.TrimSpace(viper.GetString("llm.api_key"))
	}
}

func llmModelForProvider(provider string) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "azure":
		return firstNonEmpty(
			viper.GetString("llm.azure.deployment"),
			viper.GetString("llm.model"),
		)
	default:
		return strings.TrimSpace(viper.GetString("llm.model"))
	}
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "openai"
	}
	return provider
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
