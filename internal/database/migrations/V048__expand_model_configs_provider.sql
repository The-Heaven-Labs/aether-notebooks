-- Migration 048: Expand model_configs provider values
-- Original constraint only allowed 'openai', 'openai-compatible'
-- Now supports all major LLM providers

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'model_configs_provider_check') THEN
        ALTER TABLE model_configs DROP CONSTRAINT model_configs_provider_check;
    END IF;

    ALTER TABLE model_configs ADD CONSTRAINT model_configs_provider_check
        CHECK (provider IN (
            'openai', 'anthropic', 'google', 'opencode_zen', 'opencode_go',
            'openrouter', 'ollama', 'lmstudio', 'together', 'groq',
            'fireworks', 'mistral', 'deepseek', 'other'
        ));
END $$;
