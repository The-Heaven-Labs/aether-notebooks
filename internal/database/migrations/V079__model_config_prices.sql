ALTER TABLE model_configs
    ADD COLUMN price_per_input_token NUMERIC NOT NULL DEFAULT 0,
    ADD COLUMN price_per_output_token NUMERIC NOT NULL DEFAULT 0,
    ADD COLUMN price_per_cache_read_token NUMERIC NOT NULL DEFAULT 0;
