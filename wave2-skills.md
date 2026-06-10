# Wave 2 - Skills System (Items 16, 17)

**Status:** ✅ COMPLETE (already committed by parallel agents)

## Item 16: list_skills Tool

**File:** `internal/agent/tools_agent.go`

**Implementation:**
- Added `list_skills` tool registration in `RegisterAgentTools`
- Created `makeListSkillsHandler` function that queries all skills for the org
- Returns: `{"skills": [{"id", "name", "description", "capabilities"}]}`
- The `capabilities` field contains the skill's `system_prompt` so the agent can understand what each skill does

**Commit:** `fd436ec` (included in subagent spawning fix)

## Item 17: /skill:<name> Trigger

**File:** `internal/agent/engine.go`

**Implementation:**
- Added `/skill:<name>` prefix detection in `ProcessMessage` (before chatMsgs construction)
- Extracts skill name, normalizes to lowercase-dash format
- Queries skill by normalized name: `LOWER(REPLACE(name, ' ', '-')) = $2`
- If found: injects skill's `system_prompt` as system message with `# Active Skill` header
- Strips `/skill:name` prefix from user message, stores cleaned version in session history
- If skill not found: returns error `"skill '<name>' not found"`

**Behavior:**
- `/skill:data-analyst analyze sales` → injects data-analyst skill prompt, processes "analyze sales"
- `/skill:code-reviewer` (no message) → uses "Use the skill instructions above."
- Skill prompt is injected AFTER agent's configured skill prompts, BEFORE user message
- Only affects current turn (not persisted in session history)

**Commit:** `7d012d6` (included in notebook context tool commit)

## Validation

- ✅ Go build passes
- ✅ Go vet passes
- ✅ Code compiles cleanly
- ✅ Both features integrated with existing agent system

## Notes

These items were implemented by parallel agents working on Items 30 (subagent spawning) and 31 (notebook context tool). The changes were already committed before this task could make separate edits. Verified that both implementations match the design spec.
